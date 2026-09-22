package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fredericgermain/skyhub/pkg/skyhub"
)

// blockedHandlers are never forwarded by the proxy: they restart the hub,
// drop the LAN/WAN link or are otherwise destructive. Their bodies are
// still recorded, which is exactly what is needed to reverse-engineer them.
var blockedHandlers = map[string]bool{
	"sky_lan_ip_setup.sky":      true,
	"sky_lan_ipv6_setup.sky":    true,
	"sky_wansetup.sky":          true,
	"sky_eth_setup.sky":         true,
	"sky_wireless_settings.cgi": true,
	"sky_wireless_update.cmd":   true,
	"sky_wireless_wps.cgi":      true,
	"sky_rebootinfo.cgi":        true,
	"sky_rebootCPE.html":        true,
	"sky_upload.cgi":            true,
	"sky_uploadsettings.cgi":    true,
	"sky_password.scgi":         true,
	"sky_router_settings.conf":  true,
	"sky_dynamic_dns.sky":       true,
	"sky_logout.html":           true,
}

// proxyRecord is one line of the record file.
type proxyRecord struct {
	Time     time.Time  `json:"time"`
	Method   string     `json:"method"`
	Path     string     `json:"path"`
	Blocked  bool       `json:"blocked,omitempty"`
	Form     url.Values `json:"form,omitempty"`
	Status   int        `json:"status,omitempty"`
	Location string     `json:"location,omitempty"`
	BodyHead string     `json:"body_head,omitempty"`
}

// runProxy serves the hub UI on listen without asking the browser for
// credentials, recording every form POST to recordPath (JSON lines).
func runProxy(cr skyhub.Credentials, listen, recordPath string, allowAll bool) error {
	target, err := url.Parse(cr.URL)
	if err != nil {
		return err
	}
	var recMu sync.Mutex
	var recFile *os.File
	if recordPath != "" {
		recFile, err = os.OpenFile(recordPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		defer recFile.Close()
	}
	record := func(r proxyRecord) {
		recMu.Lock()
		defer recMu.Unlock()
		b, _ := json.Marshal(r)
		fmt.Fprintf(os.Stderr, "%s %s %s status=%d loc=%s blocked=%v\n", r.Time.Format("15:04:05"), r.Method, r.Path, r.Status, r.Location, r.Blocked)
		if recFile != nil {
			recFile.Write(append(b, '\n'))
		}
	}

	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = skyhub.NewDigestTransport(cr.User, cr.Password, nil)
	dir := rp.Director
	rp.Director = func(r *http.Request) {
		dir(r)
		r.Host = target.Host
		r.Header.Del("Authorization")
		if ref := r.Header.Get("Referer"); ref != "" {
			if u, err := url.Parse(ref); err == nil {
				u.Scheme, u.Host = target.Scheme, target.Host
				r.Header.Set("Referer", u.String())
			}
		}
	}
	rp.ModifyResponse = func(resp *http.Response) error {
		if resp.Request.Method == http.MethodPost {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewReader(body))
			form, _ := resp.Request.Context().Value(formKey{}).(url.Values)
			record(proxyRecord{Time: time.Now(), Method: "POST", Path: strings.TrimPrefix(resp.Request.URL.Path, "/"), Form: form, Status: resp.StatusCode, Location: resp.Header.Get("Location"), BodyHead: headOf(body)})
		}
		return nil
	}

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			rp.ServeHTTP(w, r)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		form, _ := url.ParseQuery(string(body))
		path := strings.TrimPrefix(r.URL.Path, "/")
		todo := form.Get("todo")
		if (!allowAll && blockedHandlers[path]) || todo == "reboot" || todo == "factory" {
			record(proxyRecord{Time: time.Now(), Method: "POST", Path: path, Blocked: true, Form: form})
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "<html><body><h3>skyhub proxy: POST to %s recorded and NOT forwarded (blocked handler)</h3><pre>%s</pre><a href=\"/%s\">back</a></body></html>", path, form.Encode(), form.Get("this_file"))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r = r.WithContext(contextWithForm(r, form))
		rp.ServeHTTP(w, r)
	})
	fmt.Fprintf(os.Stderr, "skyhub proxy: http://%s/ -> %s (record: %s)\n", listen, cr.URL, recordPath)
	return http.ListenAndServe(listen, h)
}

func headOf(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
