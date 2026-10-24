package infrastructure

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestSSHValidatesHostBeforeSendingPasswordAndRedactsOutput(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	var authCalls atomic.Int32
	config := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		authCalls.Add(1)
		if string(password) != "host-secret" {
			return nil, fmt.Errorf("wrong password")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	scripts := make(chan string, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				server, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					conn.Close()
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for next := range channels {
					if next.ChannelType() != "session" {
						_ = next.Reject(ssh.UnknownChannelType, "unsupported")
						continue
					}
					channel, requests, err := next.Accept()
					if err != nil {
						return
					}
					go func() {
						defer channel.Close()
						for req := range requests {
							if req.Type != "exec" {
								_ = req.Reply(false, nil)
								continue
							}
							var command struct{ Command string }
							_ = ssh.Unmarshal(req.Payload, &command)
							if command.Command != "sh -s" {
								_ = req.Reply(false, nil)
								return
							}
							_ = req.Reply(true, nil)
							script, _ := io.ReadAll(channel)
							scripts <- string(script)
							_, _ = io.WriteString(channel, "result host-secret\n")
							_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{0}))
							return
						}
					}()
				}
			}()
		}
	}()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	number, _ := strconv.Atoi(port)
	h := Host{Address: "127.0.0.1", SSHPort: number, SSHUser: "ubuntu", HostKey: "SHA256:wrong"}
	remote := &SSHRemote{Resolve: func(context.Context, string, string, string) (string, error) {
		return `{"password":"host-secret"}`, nil
	}}
	if _, err := remote.Connect(context.Background(), h, "admin"); err == nil {
		t.Fatal("wrong fingerprint accepted")
	}
	if authCalls.Load() != 0 {
		t.Fatal("password was sent before host verification")
	}
	fingerprint, err := remote.Probe(context.Background(), h)
	if err != nil || fingerprint != ssh.FingerprintSHA256(signer.PublicKey()) {
		t.Fatal(fingerprint, err)
	}
	if authCalls.Load() != 0 {
		t.Fatal("fingerprint probe sent credentials")
	}
	h.HostKey = fingerprint
	session, err := remote.Connect(context.Background(), h, "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	out, err := session.Run(context.Background(), "printf '%s' hello\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "host-secret") || !strings.Contains(out, "[已脱敏]") {
		t.Fatal("password leaked", out)
	}
	if <-scripts != "printf '%s' hello\n" {
		t.Fatal("script was not passed through stdin")
	}
}
