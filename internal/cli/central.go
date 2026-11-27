package cli

// Spike #347: push and receive deliver local archives of a pool of hosts
// to one central archive (internal/central).

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/central"
	"github.com/seletz/total-recall/internal/logfile"
	"github.com/seletz/total-recall/internal/ops"
)

func (e *env) pushCmd() *cobra.Command {
	var to, tokenFile, caFile string
	var batch int
	var timeout time.Duration
	c := &cobra.Command{
		Use:   "push --to URL --token-file FILE",
		Short: "Deliver what is new in the local archive to a central archive (spike)",
		Long: `Deliver the sessions, events, meta records and attachments that are new in
the local archive since the last acknowledged push to the central archive at
URL (total-recall receive). Rows are sent as stored: already redacted, raw
compressed. The position is kept next to the database (push-<hash>.json) and
advances only after the receiver acknowledged a batch; an unreachable receiver
loses nothing, the next push resumes.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if to == "" || tokenFile == "" {
				return usage(errors.New("push: give --to and --token-file"))
			}
			cfg, err := e.loadConfig()
			if err != nil {
				return err
			}
			tok, err := os.ReadFile(tokenFile)
			if err != nil {
				return err
			}
			hc := &http.Client{Timeout: timeout}
			if caFile != "" {
				pem, err := os.ReadFile(caFile)
				if err != nil {
					return err
				}
				pool := x509.NewCertPool()
				if !pool.AppendCertsFromPEM(pem) {
					return fmt.Errorf("push: no certificate in %s", caFile)
				}
				hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
			}
			log := logfile.New(cfg.LogPath(), e.app.Now)
			st, err := ops.OpenWrite(cfg, log)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			client := &central.Client{URL: to, Token: strings.TrimSpace(string(tok)), HTTP: hc}
			start := time.Now()
			sum, err := central.Push(cmd.Context(), st, client, central.StatePath(cfg.DB, to), batch)
			r := sum.Received
			_, _ = fmt.Fprintf(e.out(cfg), "push: %d batches, %d sessions, %d events, %d meta records, %d blobs new centrally, %d conflicts, %s\n",
				sum.Batches, r.Sessions, r.Events, r.Meta, r.Blobs, r.Conflicts, time.Since(start).Round(time.Millisecond))
			return err
		},
	}
	f := c.Flags()
	f.StringVar(&to, "to", "", "base URL of the receiver, e.g. https://archive.example:8787")
	f.StringVar(&tokenFile, "token-file", "", "file with this host's token")
	f.StringVar(&caFile, "ca", "", "PEM file with the CA that signed the receiver's certificate (default: system roots)")
	f.IntVar(&batch, "batch", 2000, "events and meta records per request")
	f.DurationVar(&timeout, "timeout", 60*time.Second, "timeout per request")
	return c
}

func (e *env) receiveCmd() *cobra.Command {
	var listen, hostsFile, certFile, keyFile string
	c := &cobra.Command{
		Use:   "receive --listen ADDR --hosts FILE",
		Short: "Receive pushes of pool hosts into this archive (spike)",
		Long: `Serve the push protocol and write each delivery into this archive under the
host name its token belongs to. The hosts file has one "<host> <sha256 hex of
the token>" per line. With --tls-cert and --tls-key the receiver speaks HTTPS;
without, put it behind a TLS proxy or use it on localhost only.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if hostsFile == "" {
				return usage(errors.New("receive: give --hosts"))
			}
			cfg, err := e.loadConfig()
			if err != nil {
				return err
			}
			hosts, err := central.ReadHosts(hostsFile)
			if err != nil {
				return err
			}
			log := logfile.New(cfg.LogPath(), e.app.Now)
			st, err := ops.OpenWrite(cfg, log)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			out := e.out(cfg)
			srv := &central.Server{Store: st, Hosts: hosts, Logf: func(f string, a ...any) {
				_, _ = fmt.Fprintf(out, f+"\n", a...)
			}}
			hs := &http.Server{Addr: listen, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
			go func() {
				<-cmd.Context().Done()
				_ = hs.Close()
			}()
			_, _ = fmt.Fprintf(out, "receive: %s, %d hosts, archive %s\n", listen, len(hosts), cfg.DB)
			if certFile != "" {
				err = hs.ListenAndServeTLS(certFile, keyFile)
			} else {
				err = hs.ListenAndServe()
			}
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		},
	}
	f := c.Flags()
	f.StringVar(&listen, "listen", "127.0.0.1:8787", "address to listen on")
	f.StringVar(&hostsFile, "hosts", "", "file with \"<host> <sha256 of token>\" per line")
	f.StringVar(&certFile, "tls-cert", "", "certificate (PEM) for HTTPS")
	f.StringVar(&keyFile, "tls-key", "", "private key (PEM) for HTTPS")
	return c
}
