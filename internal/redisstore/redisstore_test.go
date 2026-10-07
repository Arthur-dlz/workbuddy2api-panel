package redisstore

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestNormalizeURL(t *testing.T) {
	cases := []struct {
		name  string
		url   string
		token string
		want  string
	}{
		{"完整rediss", "rediss://default:tok@host:6379", "ignored", "rediss://default:tok@host:6379"},
		{"完整redis", "redis://default:tok@host:6379", "ignored", "redis://default:tok@host:6379"},
		{"https host", "https://foo.upstash.io", "tok", "rediss://default:tok@foo.upstash.io:6379"},
		{"裸host", "foo.upstash.io", "tok", "rediss://default:tok@foo.upstash.io:6379"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := normalizeURL(c.url, c.token); got != c.want {
				t.Errorf("normalizeURL(%q,%q)=%q want %q", c.url, c.token, got, c.want)
			}
		})
	}
}

func TestNormalizeURLStripsTrailingPath(t *testing.T) {
	// 用户照抄 Upstash 控制台的 REST 地址，可能带任意路径——剥 scheme 只取 host:port 之前段。
	got := normalizeURL("https://foo.upstash.io", "t")
	if strings.Contains(got, "://foo.upstash.io") && !strings.HasSuffix(got, "foo.upstash.io:6379") {
		t.Errorf("unexpected: %s", got)
	}
}

func TestNewEmptyURLReturnsNoop(t *testing.T) {
	if _, ok := New("", "").(Noop); !ok {
		t.Fatalf("empty url should return Noop")
	}
}

func TestNewBadSchemeReturnsNoop(t *testing.T) {
	// 组装出的连接串含空格 → ParseURL 解析失败 → 降级 Noop，不 panic、不发网络请求。
	if _, ok := New("://bad host", "").(Noop); !ok {
		t.Fatalf("bad url should return Noop")
	}
}

func TestNoopMethods(t *testing.T) {
	n := Noop{}
	n.SetBind("k", "u", time.Minute) // 不 panic
	n.DelBind("k")
	n.SaveState([]byte("{}"))
	if _, ok := n.LoadState(); ok {
		t.Error("Noop.LoadState should report not-found")
	}
}

func TestBindKeyPrefix(t *testing.T) {
	if got := bindKey("abc"); got != bindPrefix+"abc" {
		t.Errorf("bindKey=%q want prefix", got)
	}
}

func TestBindMutationsPreservePerKeySubmissionOrder(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	enteredOld := make(chan struct{})
	releaseOld := make(chan struct{})
	laterBeforeRelease := make(chan string, 2)
	var once sync.Once
	var mu sync.Mutex
	values := map[string]string{}
	var clients sync.WaitGroup
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			clients.Add(1)
			go func(c net.Conn) {
				defer clients.Done()
				defer c.Close()
				br := bufio.NewReader(c)
				for {
					args, err := readRESPCommand(br)
					if err != nil {
						return
					}
					if len(args) >= 3 && strings.EqualFold(args[0], "set") && args[2] == "old" {
						once.Do(func() { close(enteredOld) })
						<-releaseOld
					} else if len(args) >= 2 && strings.EqualFold(args[0], "del") || len(args) >= 3 && strings.EqualFold(args[0], "set") && args[2] == "new" {
						select {
						case laterBeforeRelease <- strings.ToLower(args[0]):
						default:
						}
					}
					mu.Lock()
					if len(args) >= 3 && strings.EqualFold(args[0], "set") {
						values[args[1]] = args[2]
					} else if len(args) >= 2 && strings.EqualFold(args[0], "del") {
						delete(values, args[1])
					}
					mu.Unlock()
					if len(args) > 0 && strings.EqualFold(args[0], "hello") {
						_, _ = io.WriteString(c, "%7\r\n+server\r\n+redis\r\n+version\r\n+7.0.0\r\n+proto\r\n:3\r\n+id\r\n:1\r\n+mode\r\n+standalone\r\n+role\r\n+master\r\n+modules\r\n*0\r\n")
					} else if len(args) > 0 && strings.EqualFold(args[0], "del") {
						_, _ = io.WriteString(c, ":1\r\n")
					} else {
						_, _ = io.WriteString(c, "+OK\r\n")
					}
				}
			}(conn)
		}
	}()

	client := redis.NewClient(&redis.Options{Addr: ln.Addr().String(), MaxRetries: -1, Protocol: 2, DisableIdentity: true, PoolSize: 3, MaxActiveConns: 3})
	u := &Upstash{client: client, sem: make(chan struct{}, writeConcurrencyLimit), done: make(chan struct{})}
	u.SetBind("session", "old", time.Minute)
	select {
	case <-enteredOld:
	case <-time.After(2 * time.Second):
		close(releaseOld)
		t.Fatal("fake Redis did not receive first bind mutation")
	}
	u.DelBind("session")
	u.SetBind("session", "new", time.Minute)
	select {
	case op := <-laterBeforeRelease:
		close(releaseOld)
		t.Fatalf("later %s reached fake Redis before older mutation completed", op)
	case <-time.After(150 * time.Millisecond):
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- u.Close() }()
	select {
	case err := <-closeDone:
		close(releaseOld)
		t.Fatalf("Close returned before queued bind mutations drained: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseOld)
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not drain queued bind mutations")
	}
	clients.Wait()
	mu.Lock()
	got := values[bindKey("session")]
	mu.Unlock()
	if got != "new" {
		t.Fatalf("final mirrored binding=%q, want newest UID new", got)
	}
}

func readRESPCommand(br *bufio.Reader) ([]string, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 3 || line[0] != '*' {
		return nil, fmt.Errorf("invalid array header %q", line)
	}
	n, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil {
		return nil, err
	}
	args := make([]string, n)
	for i := range args {
		header, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if len(header) < 3 || header[0] != '$' {
			return nil, fmt.Errorf("invalid bulk header %q", header)
		}
		length, err := strconv.Atoi(strings.TrimSpace(header[1:]))
		if err != nil {
			return nil, err
		}
		buf := make([]byte, length+2)
		if _, err := io.ReadFull(br, buf); err != nil {
			return nil, err
		}
		args[i] = string(buf[:length])
	}
	return args, nil
}
