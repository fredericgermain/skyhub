package skyhub_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

const pingPage = `<html><form name="ping"><textarea name="ping_result">
PING 192.168.50.1 (192.168.50.1): 56 data bytes
64 bytes from 192.168.50.1: seq=0 ttl=64 time=0.293 ms
64 bytes from 192.168.50.1: seq=1 ttl=64 time=0.520 ms

--- 192.168.50.1 ping statistics ---
2 packets transmitted, 2 packets received, 0% packet loss
</textarea><input name="reflash_flag" value="0" type="hidden"/></form></html>`

func TestPing(t *testing.T) {
	h, c := newFake(t)
	h.SetFixture("sky_diagnostics_ping.html", []byte(pingPage))
	h.Handle("sky_diagnostics_ping.cgi", func(w http.ResponseWriter, r *http.Request, v url.Values) bool {
		w.Write([]byte("<html><head><meta http-equiv='refresh' content='0; URL=sky_diagnostics_ping.html' /></head></html>"))
		return true
	})
	res, err := c.Ping(context.Background(), mustAddr("192.168.50.1"))
	if err != nil {
		t.Fatal(err)
	}
	sent := h.Posts()[0].Values
	if sent.Get("c4_IPAddr") != "192.168.50.1" || sent.Get("IPAddr4") != "1" || sent.Get("todo") != "ping_test" {
		t.Errorf("sent = %v", sent)
	}
	if !res.Success || res.Sent != 2 || res.Received != 2 || len(res.RTTs) != 2 || res.RTTs[1] != 0.520 {
		t.Errorf("res = %+v", res)
	}
}

func TestDNSLookup(t *testing.T) {
	h, c := newFake(t)
	page := `<html><form name="dnslookup"><input name="sessionKey" value="1234567890" type="hidden"/><input name="todo" value="dnslookup" type="hidden"/></form>
<span class="label w250" id="dns-server-primary-text">Primary</span><span>
            203.0.113.1
            </span>
<table id="dns-lookup-output-value-table"><tbody><tr><td>  1.2.3.4
</td><td>  5.6.7.8
</td></tr></tbody></table></html>`
	h.SetFixture("sky_diagnostics.html", []byte(page))
	h.Handle("sky_diagnostics_dns.cgi", func(w http.ResponseWriter, r *http.Request, v url.Values) bool {
		w.Write([]byte("<html><head><meta http-equiv='refresh' content='0; URL=sky_diagnostics.html' /></head></html>"))
		return true
	})
	res, err := c.DNSLookup(context.Background(), "example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.Posts()[0].Values
	if sent.Get("lookup_name") != "example.com" || sent.Get("LookupType") != "ipv4" || sent.Get("todo") != "dnslookup" {
		t.Errorf("sent = %v", sent)
	}
	if len(res.Addresses) != 2 || res.Addresses[0] != "1.2.3.4" || len(res.Servers) != 1 {
		t.Errorf("res = %+v", res)
	}
}
