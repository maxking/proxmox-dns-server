package main

import (
	"net"

	"github.com/miekg/dns"
)

type testResponseWriter struct {
	msg        *dns.Msg
	remoteAddr net.Addr
}

func newTestResponseWriter() *testResponseWriter {
	return &testResponseWriter{
		remoteAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345},
	}
}

func (w *testResponseWriter) WriteMsg(m *dns.Msg) error {
	w.msg = m.Copy()
	return nil
}

func (w *testResponseWriter) Write([]byte) (int, error) {
	return 0, nil
}

func (w *testResponseWriter) Close() error { return nil }

func (w *testResponseWriter) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 53}
}

func (w *testResponseWriter) RemoteAddr() net.Addr {
	return w.remoteAddr
}

func (w *testResponseWriter) TsigStatus() error { return nil }

func (w *testResponseWriter) TsigTimersOnly(bool) {}

func (w *testResponseWriter) Hijack() {}
