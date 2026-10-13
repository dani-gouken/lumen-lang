package vm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A program is one thread: it cannot answer a request while it is doing
// something else. So the server of std.http is split in two. Go accepts
// the connections and reads the requests, each in its own goroutine, and
// queues them; the program takes them one at a time (accept), works out
// the answer, and hands it back (respond).

// exchange is one request waiting for its answer.
type exchange struct {
	request []string
	answer  chan answer
}

type answer struct {
	status  int
	headers []string // name, value, name, value, ...
	body    string
}

type httpServer struct {
	server   *http.Server
	port     int
	waiting  chan *exchange
	closed   chan struct{}
	current  *exchange // accepted, not answered yet
	isClosed bool
}

// maxBody is the most a request or a response body may weigh.
const maxBody = 32 << 20

func (s *httpServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		http.Error(w, "the request is too large", http.StatusRequestEntityTooLarge)
		return
	}
	request := []string{r.Method, r.URL.Path, r.URL.RawQuery, string(body), r.RemoteAddr}
	request = append(request, headerPairs(r.Header)...)

	waiting := &exchange{request: request, answer: make(chan answer, 1)}
	select {
	case s.waiting <- waiting:
	case <-s.closed:
		http.Error(w, "the server is closing", http.StatusServiceUnavailable)
		return
	}
	select {
	case reply := <-waiting.answer:
		for i := 0; i+1 < len(reply.headers); i += 2 {
			w.Header().Set(reply.headers[i], reply.headers[i+1])
		}
		w.WriteHeader(reply.status)
		io.WriteString(w, reply.body)
	case <-s.closed:
		http.Error(w, "the server is closing", http.StatusServiceUnavailable)
	}
}

// headerPairs lists headers as name, value, name, value, with the names
// in lower case and sorted, and the values of a repeated header joined.
func headerPairs(header http.Header) []string {
	names := make([]string, 0, len(header))
	for name := range header {
		names = append(names, name)
	}
	sort.Strings(names)
	pairs := make([]string, 0, 2*len(names))
	for _, name := range names {
		pairs = append(pairs, strings.ToLower(name), strings.Join(header[name], ", "))
	}
	return pairs
}

// kindOfNetworkError names why a request got no answer, for std.http to
// turn into its ErrorKind.
func kindOfNetworkError(err error) string {
	var dns *net.DNSError
	var network net.Error
	var address *url.Error
	switch {
	case errors.As(err, &dns):
		return "unknown host"
	case errors.As(err, &network) && network.Timeout():
		return "timeout"
	case strings.Contains(err.Error(), "connection refused"):
		return "refused"
	case errors.As(err, &address) && address.Op == "parse":
		return "bad url"
	}
	return "other"
}

func (vm *VM) httpServerOf(args []Value) (*httpServer, error) {
	id, err := argInt(args, 0)
	if err != nil {
		return nil, err
	}
	server, ok := vm.servers[id]
	if !ok {
		return nil, fmt.Errorf("there is no server %d", id)
	}
	return server, nil
}

func init() {
	registerNatives("std.http",
		// Sends a request and waits for the answer: the status, the body,
		// then the headers as name, value, name, value. Null if there was
		// no answer, the reason being kept.
		native{"exchange", 5, func(vm *VM, args []Value) (Value, error) {
			method, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			address, err := argString(args, 1)
			if err != nil {
				return NewNull(), err
			}
			headers, err := argStrings(args, 2)
			if err != nil {
				return NewNull(), err
			}
			body, err := argString(args, 3)
			if err != nil {
				return NewNull(), err
			}
			timeout, err := argInt(args, 4)
			if err != nil {
				return NewNull(), err
			}

			parsed, err := url.Parse(address)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return vm.refused("bad url", "an address starts with http:// or https:// and names a host")
			}
			request, err := http.NewRequest(method, address, strings.NewReader(body))
			if err != nil {
				return vm.refused("bad url", err.Error())
			}
			for i := 0; i+1 < len(headers); i += 2 {
				request.Header.Set(headers[i], headers[i+1])
			}

			client := &http.Client{Timeout: time.Duration(timeout) * time.Millisecond}
			response, err := client.Do(request)
			if err != nil {
				var wrapped *url.Error
				reason := err.Error()
				if errors.As(err, &wrapped) {
					reason = wrapped.Err.Error()
				}
				return vm.refused(kindOfNetworkError(err), reason)
			}
			defer response.Body.Close()
			content, err := io.ReadAll(io.LimitReader(response.Body, maxBody))
			if err != nil {
				return vm.refused(kindOfNetworkError(err), err.Error())
			}
			parts := []string{strconv.Itoa(response.StatusCode), string(content)}
			return vm.newStringArray(append(parts, headerPairs(response.Header)...))
		}},

		// Starts listening on a port of this machine; 0 lets the system
		// choose one. Gives the server's number and its port, or null.
		native{"open", 1, func(vm *VM, args []Value) (Value, error) {
			port, err := argInt(args, 0)
			if err != nil {
				return NewNull(), err
			}
			listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				if strings.Contains(err.Error(), "address already in use") {
					return vm.refused("in use", "the port is taken")
				}
				var wrapped *net.OpError
				reason := err.Error()
				if errors.As(err, &wrapped) {
					reason = wrapped.Err.Error()
				}
				return vm.refused("other", reason)
			}
			server := &httpServer{
				port:    listener.Addr().(*net.TCPAddr).Port,
				waiting: make(chan *exchange),
				closed:  make(chan struct{}),
			}
			server.server = &http.Server{Handler: server}
			go server.server.Serve(listener)

			if vm.servers == nil {
				vm.servers = map[int64]*httpServer{}
			}
			vm.lastServer++
			vm.servers[vm.lastServer] = server
			return vm.newIntArray([]int64{vm.lastServer, int64(server.port)})
		}},
		// The next request: its method, path, query, body and sender, then
		// its headers. Waits for one, at most `timeout` milliseconds if
		// that is not 0. Null if none came, or the server is closed.
		native{"accept", 2, func(vm *VM, args []Value) (Value, error) {
			server, err := vm.httpServerOf(args)
			if err != nil {
				return NewNull(), err
			}
			timeout, err := argInt(args, 1)
			if err != nil {
				return NewNull(), err
			}
			if server.current != nil {
				return NewNull(), errors.New("the request before this one has not been answered")
			}
			var limit <-chan time.Time
			if timeout > 0 {
				limit = time.After(time.Duration(timeout) * time.Millisecond)
			}
			select {
			case next := <-server.waiting:
				server.current = next
				return vm.newStringArray(next.request)
			case <-limit:
			case <-server.closed:
			}
			return NewNull(), nil
		}},
		// Answers the request accept gave
		native{"respondTo", 4, func(vm *VM, args []Value) (Value, error) {
			server, err := vm.httpServerOf(args)
			if err != nil {
				return NewNull(), err
			}
			status, err := argInt(args, 1)
			if err != nil {
				return NewNull(), err
			}
			headers, err := argStrings(args, 2)
			if err != nil {
				return NewNull(), err
			}
			body, err := argString(args, 3)
			if err != nil {
				return NewNull(), err
			}
			if server.current == nil {
				return NewNull(), errors.New("there is no request to answer")
			}
			if status < 100 || status > 599 {
				return NewNull(), fmt.Errorf("%d is not a status a server can answer with", status)
			}
			server.current.answer <- answer{status: int(status), headers: headers, body: body}
			server.current = nil
			return NewNull(), nil
		}},
		native{"shut", 1, func(vm *VM, args []Value) (Value, error) {
			server, err := vm.httpServerOf(args)
			if err != nil {
				return NewNull(), err
			}
			if !server.isClosed {
				server.isClosed = true
				close(server.closed)
				// Lets the answers already given reach their senders
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				server.server.Shutdown(ctx)
				cancel()
			}
			return NewNull(), nil
		}},
		problemNative,
	)
}
