package collector_test

import (
	"crypto/md5" //nolint:gosec // RFC 2617 digest authentication is specified with MD5.
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mhttp "github.com/mathieumaf/go-monero/pkg/http"
	"github.com/mathieumaf/go-monero/pkg/rpc"
	"github.com/mathieumaf/go-monero/pkg/rpc/daemon"

	"github.com/mathieumaf/monero-exporter/pkg/collector"
)

// fixedNowUnix is the clock pinned for this suite (2026-01-15 12:00:00 UTC).
// Timestamps in the monerod fixtures are offsets from this instant.
const fixedNowUnix int64 = 1_768_478_400

const (
	goldenPath   = "testdata/metrics.golden"
	baselinePath = "testdata/promtool_baseline.txt"
	fixtureDir   = "testdata/monerod/v0.18.4.4"

	digestRealm = "monero-rpc"
	digestNonce = "fixture-nonce"
	digestUser  = "exporter"
	digestPass  = "fixture-password"
)

// digestChallenge is the five-field challenge go-monero's digest transport
// accepts: qop, algorithm, realm, nonce and stale, with no spaces after commas.
const digestChallenge = `Digest qop="auth",algorithm="MD5",` +
	`realm="monero-rpc",nonce="fixture-nonce",stale="false"`

var (
	updateGolden = flag.Bool(
		"update", false,
		"rewrite the golden /metrics file and the promtool baseline",
	)

	errEmptyMethod = errors.New("empty jsonrpc method")
	errAnchors     = errors.New("fixture is missing the block hash or tx hashes")
	errMissingJSON = errors.New("missing json field")

	ipv4Pattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

	docNets = mustNets("192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24")
)

func TestMain(m *testing.M) {
	flag.Parse()

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		*updateGolden = true
	}

	restore := collector.SetNowForTest(func() time.Time {
		return time.Unix(fixedNowUnix, 0).UTC()
	})
	code := m.Run()
	restore()
	os.Exit(code)
}

func mustNets(cidrs ...string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))

	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err)
		}

		nets = append(nets, network)
	}

	return nets
}

type fakeMonerod struct {
	bodies    map[string][]byte
	status    map[string]int
	seen      map[string]int
	mu        sync.Mutex
	user      string
	pass      string
	blockHash string
	txHashes  []string
	challenge atomic.Int64
	authed    atomic.Int64
	srv       *httptest.Server
}

func newFakeMonerod(t *testing.T, opts ...func(*fakeMonerod)) *fakeMonerod {
	t.Helper()

	srv := &fakeMonerod{
		bodies: loadFixtures(t),
		status: map[string]int{},
		seen:   map[string]int{},
	}
	if err := srv.learnAnchors(); err != nil {
		t.Fatalf("fixture anchors: %v", err)
	}

	for _, opt := range opts {
		opt(srv)
	}

	srv.srv = httptest.NewServer(srv)
	t.Cleanup(srv.srv.Close)

	return srv
}

func withDigest(user, pass string) func(*fakeMonerod) {
	return func(s *fakeMonerod) {
		s.user = user
		s.pass = pass
	}
}

func withStatus(key string, code int) func(*fakeMonerod) {
	return func(s *fakeMonerod) {
		s.status[key] = code
	}
}

func withBody(key string, body []byte) func(*fakeMonerod) {
	return func(s *fakeMonerod) {
		s.bodies[key] = append([]byte(nil), body...)
	}
}

func (s *fakeMonerod) client(t *testing.T, user, pass string) *daemon.Client {
	t.Helper()

	httpClient, err := mhttp.NewClient(mhttp.ClientConfig{
		Username: user,
		Password: pass,
	})
	if err != nil {
		t.Fatalf("http client: %v", err)
	}

	rpcClient, err := rpc.NewClient(s.srv.URL, rpc.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("rpc client: %v", err)
	}

	return daemon.NewClient(rpcClient)
}

func (s *fakeMonerod) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.authenticate(w, r) {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)

		return
	}

	key, err := routeKey(r.URL.Path, body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)

		return
	}

	payload, code, ok := s.take(key)
	if !ok {
		http.Error(w, "unknown "+key, http.StatusNotFound)

		return
	}

	if code != 0 {
		writeStatus(w, code)

		return
	}

	if !s.requestOK(key, body) {
		http.Error(w, "unexpected request for "+key, http.StatusBadRequest)

		return
	}

	writePayload(w, r.URL.Path, payload)
}

func (s *fakeMonerod) authenticate(w http.ResponseWriter, r *http.Request) bool {
	if s.user == "" {
		return true
	}

	header := r.Header.Get("Authorization")
	if digestOK(s.user, s.pass, r.Method, r.URL.RequestURI(), header) {
		s.authed.Add(1)

		return true
	}

	w.Header().Set("WWW-Authenticate", digestChallenge)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = io.WriteString(w, "unauthorized\n")
	s.challenge.Add(1)

	return false
}

func (s *fakeMonerod) take(key string) (body []byte, code int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seen[key]++
	body, ok = s.bodies[key]
	code = s.status[key]

	return body, code, ok
}

// assertSeenAll requires every fixture endpoint to have been called, except
// skip. Skipped endpoints must not have been called: the default collection
// avoids get_last_block_header when get_info already has the tip, and it does
// not call get_transaction_pool unless the full pool is opted in.
func (s *fakeMonerod) assertSeenAll(t *testing.T, skip ...string) {
	t.Helper()

	skipped := make(map[string]struct{}, len(skip))
	for _, name := range skip {
		skipped[name] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for key := range s.bodies {
		if _, ok := skipped[key]; ok {
			if s.seen[key] != 0 {
				t.Errorf("monerod method %s was called %d times", key, s.seen[key])
			}

			continue
		}

		if s.seen[key] == 0 {
			t.Errorf("monerod method %s was not called", key)
		}
	}
}

func (s *fakeMonerod) learnAnchors() error {
	header, err := jsonField(s.bodies["get_last_block_header"], "block_header")
	if err != nil {
		return fmt.Errorf("header: %w", err)
	}

	hash, err := jsonString(header, "hash")
	if err != nil {
		return fmt.Errorf("block hash: %w", err)
	}

	encoded, err := jsonString(s.bodies["get_block"], "json")
	if err != nil {
		return fmt.Errorf("block json: %w", err)
	}

	hashes, err := jsonStringList([]byte(encoded), "tx_hashes")
	if err != nil {
		return fmt.Errorf("tx hashes: %w", err)
	}

	s.blockHash = hash
	s.txHashes = hashes
	if s.blockHash == "" || len(s.txHashes) == 0 {
		return errAnchors
	}

	return nil
}

func (s *fakeMonerod) requestOK(key string, body []byte) bool {
	switch key {
	case "get_block":
		return hashParam(body) == s.blockHash
	case "get_transactions":
		return txsRequestOK(body, s.txHashes)
	default:
		return true
	}
}

func routeKey(path string, body []byte) (string, error) {
	if path != "/json_rpc" {
		return strings.TrimPrefix(path, "/"), nil
	}

	var env struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return "", fmt.Errorf("decode jsonrpc: %w", err)
	}

	if env.Method == "" {
		return "", errEmptyMethod
	}

	return env.Method, nil
}

func hashParam(body []byte) string {
	params, err := jsonField(body, "params")
	if err != nil {
		return ""
	}

	hash, err := jsonString(params, "hash")
	if err != nil {
		return ""
	}

	return hash
}

func txsRequestOK(body []byte, want []string) bool {
	decode, err := jsonBool(body, "decode_as_json")
	if err != nil || !decode {
		return false
	}

	hashes, err := jsonStringList(body, "txs_hashes")
	if err != nil {
		return false
	}

	return slices.Equal(hashes, want)
}

func jsonField(body []byte, key string) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("decode object: %w", err)
	}

	raw, ok := obj[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errMissingJSON, key)
	}

	return raw, nil
}

func jsonString(body []byte, key string) (string, error) {
	raw, err := jsonField(body, key)
	if err != nil {
		return "", err
	}

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("decode %s: %w", key, err)
	}

	return value, nil
}

func jsonStringList(body []byte, key string) ([]string, error) {
	raw, err := jsonField(body, key)
	if err != nil {
		return nil, err
	}

	var value []string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("decode %s: %w", key, err)
	}

	return value, nil
}

func jsonBool(body []byte, key string) (bool, error) {
	raw, err := jsonField(body, key)
	if err != nil {
		return false, err
	}

	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fmt.Errorf("decode %s: %w", key, err)
	}

	return value, nil
}

func writeStatus(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, `{"status":"FIXTURE_ERROR"}`)
}

func writePayload(w http.ResponseWriter, path string, payload []byte) {
	w.Header().Set("Content-Type", "application/json")

	if path == "/json_rpc" {
		_, _ = fmt.Fprintf(w, `{"id":"0","jsonrpc":"2.0","result":%s}`, payload)

		return
	}

	_, _ = w.Write(payload)
}

func digestOK(user, pass, method, uri, header string) bool {
	params := digestParams(header)
	if params == nil || params["username"] != user || params["realm"] != digestRealm {
		return false
	}

	if params["nonce"] != digestNonce || params["qop"] != "auth" || params["uri"] != uri {
		return false
	}

	ha1 := hashMD5(user + ":" + digestRealm + ":" + pass)
	ha2 := hashMD5(method + ":" + uri)
	want := hashMD5(strings.Join(
		[]string{ha1, params["nonce"], params["nc"], params["cnonce"], params["qop"], ha2},
		":",
	))

	got := params["response"]
	if len(got) != len(want) {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

func digestParams(header string) map[string]string {
	const prefix = "Digest "

	if !strings.HasPrefix(header, prefix) {
		return nil
	}

	out := map[string]string{}

	for field := range strings.SplitSeq(header[len(prefix):], ",") {
		key, val, ok := strings.Cut(strings.TrimSpace(field), "=")
		if !ok {
			return nil
		}

		out[key] = strings.Trim(val, `"`)
	}

	return out
}

func hashMD5(data string) string {
	sum := md5.Sum([]byte(data)) //nolint:gosec // RFC 2617 digest authentication uses MD5

	return hex.EncodeToString(sum[:])
}

func loadFixtures(t *testing.T) map[string][]byte {
	t.Helper()

	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}

	out := make(map[string][]byte, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		path := filepath.Join(fixtureDir, entry.Name())

		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}

		name := strings.TrimSuffix(entry.Name(), ".json")
		out[name] = body
	}

	for _, name := range fixtureNames {
		if _, ok := out[name]; !ok {
			t.Fatalf("missing fixture %s", name)
		}
	}

	return out
}

const rpcGetInfo = "get_info"

var fixtureNames = []string{
	"get_block",
	"get_connections",
	rpcGetInfo,
	"get_last_block_header",
	"get_net_stats",
	"get_peer_list",
	"get_transaction_pool",
	"get_transaction_pool_stats",
	"get_transactions",
	"rpc_access_tracking",
}

func mustJSON(t *testing.T, body []byte, dest any) {
	t.Helper()

	if err := json.Unmarshal(body, dest); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}
