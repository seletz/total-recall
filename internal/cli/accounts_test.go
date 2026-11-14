package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// accountsEnv is a test environment on the demo archive with its logins
// named and grouped by demo.AccountsTOML.
func accountsEnv(t *testing.T) (*testEnv, string) {
	t.Helper()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	e.home = demo.Home
	appendConfig(t, e, demo.AccountsTOML)
	return e, db
}

// accSeries is the part of the series JSON the sums need.
type accSeries struct {
	By    *string `json:"by"`
	Keys  []string
	Total struct{ Sessions, Human, Agent int }
	ByKey []struct {
		Key                    string
		Sessions, Human, Agent int
	} `json:"by_key"`
}

// accRange covers the whole demo archive.
var accRange = []string{"--since", "2026-01-01", "--until", "2026-12-31"}

func series(t *testing.T, e *testEnv, db string, extra ...string) accSeries {
	t.Helper()
	args := append([]string{"--db", db, "stats", "--json"}, accRange...)
	return decode[accSeries](t, e.mustRun(t, append(args, extra...)...))
}

// TestStatsAccountSumsEqualTotal: on the real --json output of the demo
// archive, the keys of --by account and of --by account-group add up to the
// total without --by (sessions, human and agent events), and the total of
// the series is the total of the plain tables.
func TestStatsAccountSumsEqualTotal(t *testing.T) {
	t.Parallel()
	e, db := accountsEnv(t)
	plain := decode[struct {
		Total struct{ Sessions, Human, Agent int }
	}](t, e.mustRun(t, append([]string{"--db", db, "stats", "--json"}, accRange...)...))
	whole := series(t, e, db, "--period", "month")
	if whole.Total.Sessions == 0 || whole.Total.Human == 0 || whole.Total.Agent == 0 {
		t.Fatalf("total = %+v, the demo archive should have activity", whole.Total)
	}
	if whole.Total != plain.Total {
		t.Errorf("series total %+v != plain total %+v", whole.Total, plain.Total)
	}
	for _, by := range []string{"account", "account-group"} {
		t.Run(by, func(t *testing.T) {
			s := series(t, e, db, "--period", "month", "--by", by)
			var sum struct{ Sessions, Human, Agent int }
			for _, k := range s.ByKey {
				sum.Sessions += k.Sessions
				sum.Human += k.Human
				sum.Agent += k.Agent
			}
			if sum != whole.Total {
				t.Errorf("--by %s: keys add up to %+v, total is %+v (keys %v)", by, sum, whole.Total, s.Keys)
			}
		})
	}
	// the keys are the names, and the company logins share a group
	acc := series(t, e, db, "--by", "account")
	for _, want := range []string{"kompass-team", "kompass-solo", "privat", "unknown"} {
		if !contains(acc.Keys, want) {
			t.Errorf("--by account keys %v lack %q", acc.Keys, want)
		}
	}
	grp := series(t, e, db, "--by", "account-group")
	if got := strings.Join(grp.Keys, ","); len(grp.Keys) != 3 || !contains(grp.Keys, "firma") || !contains(grp.Keys, "privat") || !contains(grp.Keys, "unknown") {
		t.Errorf("--by account-group keys = %s, want firma, privat and unknown", got)
	}
	// firma is the sum of the two company logins
	var firma, parts int
	for _, k := range grp.ByKey {
		if k.Key == "firma" {
			firma = k.Human + k.Agent
		}
	}
	for _, k := range acc.ByKey {
		if k.Key == "kompass-team" || k.Key == "kompass-solo" {
			parts += k.Human + k.Agent
		}
	}
	if firma == 0 || firma != parts {
		t.Errorf("firma = %d events, its logins add up to %d", firma, parts)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// TestStatsAccountGroupFilter: --account-group keeps one group in the plain
// tables and the series, and the filtered totals are the group's key of the
// unfiltered series.
func TestStatsAccountGroupFilter(t *testing.T) {
	t.Parallel()
	e, db := accountsEnv(t)
	grp := series(t, e, db, "--by", "account-group")
	for _, g := range []string{"firma", "privat"} {
		var want struct{ Sessions, Human, Agent int }
		for _, k := range grp.ByKey {
			if k.Key == g {
				want.Sessions, want.Human, want.Agent = k.Sessions, k.Human, k.Agent
			}
		}
		plain := decode[struct {
			Total struct{ Sessions, Human, Agent int }
		}](t, e.mustRun(t, append([]string{"--db", db, "stats", "--json", "--account-group", g}, accRange...)...))
		if plain.Total != want || want.Human == 0 {
			t.Errorf("stats --account-group %s: total %+v, group key has %+v", g, plain.Total, want)
		}
		ser := series(t, e, db, "--account-group", g, "--by", "account-group")
		if len(ser.ByKey) != 1 || ser.ByKey[0].Key != g {
			t.Errorf("--account-group %s --by account-group: keys %v", g, ser.Keys)
		}
	}
}

type accSession struct {
	ID             string  `json:"id"`
	ParentID       *string `json:"parent_id"`
	AccountID      *string `json:"account_id"`
	OrganizationID *string `json:"organization_id"`
	Account        *string `json:"account"`
	Organization   *string `json:"organization"`
	AccountGroup   *string `json:"account_group"`
}

func str(p *string) string {
	if p == nil {
		return "<null>"
	}
	return *p
}

// TestSessionsAccountOrganizations: two logins with the same account ID and
// different organizations get different names and the same group; the group
// filter keeps both; subagent sessions take their parent's login.
func TestSessionsAccountOrganizations(t *testing.T) {
	t.Parallel()
	e, db := accountsEnv(t)
	run := func(extra ...string) []accSession {
		return decode[[]accSession](t, e.mustRun(t, append([]string{"--db", db, "sessions", "--json", "--subagents", "--limit", "0"}, extra...)...))
	}
	all := run()
	names := map[string]map[string]bool{} // account ID -> names
	for _, s := range all {
		if s.ParentID == nil && s.AccountID != nil {
			if names[*s.AccountID] == nil {
				names[*s.AccountID] = map[string]bool{}
			}
			names[*s.AccountID][str(s.Account)] = true
		}
	}
	if n := names[demo.AccountTeam]; len(n) != 2 || !n["kompass-team"] || !n["kompass-solo"] {
		t.Errorf("names on the team account ID = %v, want kompass-team and kompass-solo", n)
	}
	inherited := 0
	for _, s := range all {
		if s.ParentID != nil {
			if s.AccountID != nil {
				t.Fatalf("subagent %s has its own account_id", s.ID)
			}
			if s.Account != nil || s.AccountGroup != nil {
				inherited++
			}
		}
	}
	if inherited == 0 {
		t.Error("no subagent session shows the login of its parent")
	}
	firma := run("--account-group", "firma")
	if len(firma) == 0 {
		t.Fatal("no session in group firma")
	}
	seen := map[string]bool{}
	for _, s := range firma {
		if str(s.AccountGroup) != "firma" {
			t.Errorf("session %s in the firma list has group %s", s.ID, str(s.AccountGroup))
		}
		seen[str(s.Account)] = true
	}
	if !seen["kompass-team"] || !seen["kompass-solo"] || len(seen) != 2 {
		t.Errorf("--account-group firma shows %v", seen)
	}
	// every session is in exactly one of firma, privat, none
	privat := run("--account-group", "privat")
	var none int
	for _, s := range all {
		if s.AccountGroup == nil {
			none++
		}
	}
	if len(firma)+len(privat)+none != len(all) {
		t.Errorf("firma %d + privat %d + ungrouped %d != all %d", len(firma), len(privat), none, len(all))
	}
	// --account takes the name of a pair, an account or a prefix
	if solo := run("--account", "kompass-solo"); len(solo) == 0 || len(solo) >= len(firma) {
		t.Errorf("--account kompass-solo: %d sessions, firma has %d", len(solo), len(firma))
	}
	if byID := run("--account", demo.AccountTeam[:8]); len(byID) != len(firma) {
		t.Errorf("--account <id prefix>: %d sessions, want the %d of firma", len(byID), len(firma))
	}
	if both := run("--account", "kompass-solo", "--account-group", "privat"); len(both) != 0 {
		t.Errorf("solo ∩ privat = %d sessions, want 0", len(both))
	}
	// the text list has the names
	if out := e.mustRun(t, "--db", db, "sessions", "--limit", "0"); !strings.Contains(out, "ACCOUNT") || !strings.Contains(out, "kompass-solo") {
		t.Errorf("sessions text lacks the ACCOUNT column:\n%.400s", out)
	}
}

func TestAccountGroupErrors(t *testing.T) {
	t.Parallel()
	e, db := accountsEnv(t)
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"sessions", "--account-group", "nope"}, `unknown account group "nope" (configured: firma, privat)`},
		{[]string{"search", "x", "--account-group", "nope"}, "unknown account group"},
		{[]string{"stats", "--account-group", "nope"}, "unknown account group"},
		{[]string{"stats", "--tools", "--account-group", "firma"}, "cannot be combined"},
		{[]string{"search", "x", "--type", "memory", "--account-group", "firma"}, "does not apply to type memory"},
	} {
		r := e.run(t, "", append([]string{"--db", db}, tt.args...)...)
		if r.code == 0 || !strings.Contains(r.stderr, tt.want) {
			t.Errorf("%v: exit %d, stderr %q, want %q", tt.args, r.code, r.stderr, tt.want)
		}
	}
}

// TestSearchAccountGroup: search keeps the hits of the sessions of one group.
func TestSearchAccountGroup(t *testing.T) {
	t.Parallel()
	e, db := accountsEnv(t)
	type hit struct {
		SessionID string `json:"session_id"`
	}
	all := decode[[]hit](t, e.mustRun(t, "--db", db, "search", "--json", "--limit", "0", "fix"))
	firma := decode[[]hit](t, e.mustRun(t, "--db", db, "search", "--json", "--limit", "0", "fix", "--account-group", "firma"))
	privat := decode[[]hit](t, e.mustRun(t, "--db", db, "search", "--json", "--limit", "0", "fix", "--account-group", "privat"))
	if len(all) == 0 || len(firma) == 0 || len(privat) == 0 || len(firma)+len(privat) > len(all) {
		t.Errorf("search fix: all %d, firma %d, privat %d", len(all), len(firma), len(privat))
	}
	sessions := decode[[]accSession](t, e.mustRun(t, "--db", db, "sessions", "--json", "--subagents", "--limit", "0", "--account-group", "privat"))
	in := map[string]bool{}
	for _, s := range sessions {
		in[s.ID] = true
	}
	for _, h := range privat {
		if !in[h.SessionID] {
			t.Errorf("hit in %s is not a privat session", h.SessionID)
		}
	}
}

// TestShowAccountHeader: the header of show names the login, the
// organization and the group; a subagent session says it took them from its
// parent.
func TestShowAccountHeader(t *testing.T) {
	t.Parallel()
	e, db := accountsEnv(t)
	list := decode[[]accSession](t, e.mustRun(t, "--db", db, "sessions", "--json", "--subagents", "--limit", "0", "--account", "kompass-solo"))
	var main, sub string
	for _, s := range list {
		if s.ParentID == nil && main == "" {
			main = s.ID
		}
		if s.ParentID != nil && sub == "" {
			sub = s.ID
		}
	}
	out := e.mustRun(t, "--db", db, "show", main, "--limit", "1")
	for _, want := range []string{"kompass-solo (" + demo.AccountTeam + ")", demo.OrganizationSolo, "account group", "firma"} {
		if !strings.Contains(out, want) {
			t.Errorf("show header lacks %q:\n%.600s", want, out)
		}
	}
	if sub != "" {
		if out := e.mustRun(t, "--db", db, "show", sub, "--limit", "1"); !strings.Contains(out, "from the parent session") {
			t.Errorf("subagent header lacks the note:\n%.600s", out)
		}
	}
	js := decode[struct{ Session accSession }](t, e.mustRun(t, "--db", db, "show", main, "--json", "--limit", "1"))
	if str(js.Session.Account) != "kompass-solo" || str(js.Session.AccountGroup) != "firma" {
		t.Errorf("show --json session = %+v", js.Session)
	}
}

// TestAccountRegistry: names and groups come from a login registry whose
// IDs are read from the .claude.json of each login directory; the config
// wins over the registry; the registry files are not touched.
func TestAccountRegistry(t *testing.T) {
	t.Parallel()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	dir := t.TempDir()
	login := func(name, account, org string) {
		d := filepath.Join(dir, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		body := `{"oauthAccount":{"accountUuid":"` + account + `","organizationUuid":"` + org + `"}}`
		if err := os.WriteFile(filepath.Join(d, ".claude.json"), []byte(body), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	login("team", demo.AccountTeam, demo.OrganizationTeam)
	login("solo", demo.AccountTeam, demo.OrganizationSolo)
	login("home", demo.AccountPersonal, demo.OrganizationPersonal)
	reg := filepath.Join(dir, "registry.json")
	regBody := `{"accounts":{
	  "kompass-team":{"group":"firma","label":"Kompass Team","short":"kt","dir":"` + filepath.Join(dir, "team") + `"},
	  "kompass-solo":{"group":"firma","dir":"` + filepath.Join(dir, "solo") + `"},
	  "privat":{"group":"privat","dir":"` + filepath.Join(dir, "home") + `"},
	  "gone":{"group":"x","dir":"` + filepath.Join(dir, "gone") + `"}}}`
	if err := os.WriteFile(reg, []byte(regBody), 0o444); err != nil {
		t.Fatal(err)
	}
	// a root-level key must precede the tables of the base config
	cfg, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.configPath, append([]byte("account_registry = "+quote(reg)+"\n"), cfg...), 0o644); err != nil {
		t.Fatal(err)
	}
	s := decode[accSeries](t, e.mustRun(t, append([]string{"--db", db, "stats", "--json", "--by", "account-group"}, accRange...)...))
	for _, want := range []string{"firma", "privat"} {
		if !contains(s.Keys, want) {
			t.Errorf("registry groups: keys %v lack %q", s.Keys, want)
		}
	}
	r := e.run(t, "", append([]string{"--db", db, "stats", "--json", "--by", "account"}, accRange...)...)
	if r.code != 0 || r.stderr != "" {
		t.Errorf("stats: exit %d, stderr %q: registry warnings belong to doctor, not to every command", r.code, r.stderr)
	}
	if d := e.run(t, "", "--db", db, "doctor", "--days", "0"); !strings.Contains(d.stdout, "no login IDs for gone") || strings.Contains(d.stdout+d.stderr, dir) {
		t.Errorf("doctor: exit %d, want a hint about the entry gone without the path:\n%s%s", d.code, d.stdout, d.stderr)
	}
	rep := decode[[]struct {
		AccountHints []string `json:"account_hints"`
	}](t, e.run(t, "", "--db", db, "doctor", "--days", "0", "--json").stdout)
	if len(rep) == 0 || len(rep[0].AccountHints) != 1 {
		t.Errorf("doctor --json account_hints = %+v", rep)
	}
	for _, want := range []string{"kompass-team", "kompass-solo", "privat"} {
		if !strings.Contains(r.stdout, `"`+want+`"`) {
			t.Errorf("registry names: stdout lacks %q", want)
		}
	}
	// a registry that is not there is a config error
	bad := newTestEnv(t)
	bad.now = demo.Now
	if err := os.WriteFile(bad.configPath, []byte("account_registry = \"/nonexistent/registry.json\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := bad.run(t, "", "--db", db, "sessions"); r.code == 0 {
		t.Errorf("a missing registry did not fail:\n%s", r.stdout)
	}
}

func quote(s string) string { return `"` + s + `"` }

// TestMCPAccountGroup: the MCP tools take account_group and by=account-group.
func TestMCPAccountGroup(t *testing.T) {
	t.Parallel()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	appendConfig(t, e, demo.AccountsTOML)
	s := &mcpSession{t: t, e: e, args: []string{"--db", db}}
	out, isErr := s.call("sessions", map[string]any{"account_group": "privat", "limit": 500})
	if isErr || len(out) == 0 || !strings.Contains(out[0], `"account_group": "privat"`) && !strings.Contains(out[0], `"account_group":"privat"`) {
		t.Fatalf("sessions: err=%v %.300v", isErr, out)
	}
	if strings.Contains(out[0], "kompass-team") {
		t.Error("sessions with account_group privat list a firma login")
	}
	out, isErr = s.call("stats", map[string]any{"by": "account-group", "since": "2026-01-01", "until": "2026-12-31"})
	if isErr || !strings.Contains(out[0], "firma") {
		t.Fatalf("stats by account-group: err=%v %.300v", isErr, out)
	}
	out, isErr = s.call("search", map[string]any{"query": "fix", "account_group": "firma", "limit": 5})
	if isErr || len(out) == 0 {
		t.Fatalf("search: err=%v %v", isErr, out)
	}
}

// TestAccountRegistryDefaultDir: a registry entry for the default directory
// (~/.claude) takes the login from $HOME/.claude.json, not from a stale
// $HOME/.claude/.claude.json; the organization name comes from the same
// file; show names the organization; entries with the same login warn.
func TestAccountRegistryDefaultDir(t *testing.T) {
	t.Parallel()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	home := t.TempDir()
	e.home = home
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".claude.json"), `{"oauthAccount":{"accountUuid":"`+demo.AccountTeam+`","organizationUuid":"`+demo.OrganizationSolo+`","organizationName":"Wetterwarte Einzel"}}`)
	write(filepath.Join(home, ".claude", ".claude.json"), `{"oauthAccount":{"accountUuid":"`+demo.AccountTeam+`","organizationUuid":"`+demo.OrganizationTeam+`"}}`)
	write(filepath.Join(home, "dup", ".claude.json"), `{"oauthAccount":{"accountUuid":"`+demo.AccountTeam+`","organizationUuid":"`+demo.OrganizationSolo+`"}}`)
	reg := filepath.Join(home, "registry.json")
	write(reg, `{"accounts":{"default":{"group":"firma","dir":"~/.claude"},"zweit":{"group":"x","dir":"~/dup"}}}`)
	cfg, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.configPath, append([]byte("account_registry = "+quote(reg)+"\n"), cfg...), 0o644); err != nil {
		t.Fatal(err)
	}
	r := e.run(t, "", append([]string{"--db", db, "stats", "--json", "--by", "account"}, accRange...)...)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	s := decode[accSeries](t, r.stdout)
	if !contains(s.Keys, "default") || contains(s.Keys, demo.AccountTeam+"/"+demo.OrganizationSolo) {
		t.Errorf("keys = %v, want the solo login named default", s.Keys)
	}
	if contains(s.Keys, "zweit") || r.stderr != "" {
		t.Errorf("collision: keys %v, stderr %q", s.Keys, r.stderr)
	}
	if d := e.run(t, "", "--db", db, "doctor", "--days", "0"); !strings.Contains(d.stdout, "default") || !strings.Contains(d.stdout, "zweit") {
		t.Errorf("doctor does not report the collision:\n%s", d.stdout)
	}
	list := decode[[]accSession](t, e.mustRun(t, "--db", db, "sessions", "--json", "--limit", "0", "--account", "default"))
	if len(list) == 0 {
		t.Fatal("no session of the default login")
	}
	for _, x := range list {
		if str(x.OrganizationID) != demo.OrganizationSolo {
			t.Errorf("session %s on organization %s", x.ID, str(x.OrganizationID))
		}
	}
	out := e.mustRun(t, "--db", db, "show", list[0].ID, "--limit", "1")
	if !strings.Contains(out, "Wetterwarte Einzel ("+demo.OrganizationSolo+")") {
		t.Errorf("show header lacks the organization name:\n%.500s", out)
	}
	// without any name the header shows the ID, not null
	e2, db2 := accountsEnv(t)
	plain := decode[[]accSession](t, e2.mustRun(t, "--db", db2, "sessions", "--json", "--limit", "1", "--account", demo.AccountPersonal[:8]))
	if out := e2.mustRun(t, "--db", db2, "show", plain[0].ID, "--limit", "1"); !regexp.MustCompile(`organization\s+` + demo.OrganizationPersonal).MatchString(out) {
		t.Errorf("organization line without a name:\n%.500s", out)
	}
}

// TestAccountRegistryPick: an alias ("pick": false) on the same login as a
// pickable entry loses, and nothing is reported.
func TestAccountRegistryPick(t *testing.T) {
	t.Parallel()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "l"), 0o755); err != nil {
		t.Fatal(err)
	}
	login := `{"oauthAccount":{"accountUuid":"` + demo.AccountPersonal + `","organizationUuid":"` + demo.OrganizationPersonal + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "l", ".claude.json"), []byte(login), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := filepath.Join(dir, "registry.json")
	body := `{"accounts":{"default":{"group":"alias","dir":"` + filepath.Join(dir, "l") + `","pick":false},"teams":{"group":"privat","dir":"` + filepath.Join(dir, "l") + `"}}}`
	if err := os.WriteFile(reg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.configPath, append([]byte("account_registry = "+quote(reg)+"\n"), cfg...), 0o644); err != nil {
		t.Fatal(err)
	}
	s := decode[accSeries](t, e.mustRun(t, append([]string{"--db", db, "stats", "--json", "--by", "account"}, accRange...)...))
	if !contains(s.Keys, "teams") || contains(s.Keys, "default") {
		t.Errorf("keys = %v, want the login named teams", s.Keys)
	}
	if d := e.run(t, "", "--db", db, "doctor", "--days", "0"); strings.Contains(d.stdout+d.stderr, "account registry") {
		t.Errorf("doctor reports a resolved case:\n%s%s", d.stdout, d.stderr)
	}
}
