package star

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestParseModel(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       Model
		err        string
	}{
		{"view default", "SELECT 1 AS x", Model{Name: "m", Materialized: "view"}, ""},
		{"table", "{{ config(materialized='table') }}\nSELECT 1", Model{Name: "m", Materialized: "table"}, ""},
		{"incremental", `{{ config(materialized="incremental", unique_key="k") }} SELECT * FROM {{ ref('a') }} JOIN {{ ref("b") }}`,
			Model{Name: "m", Materialized: "incremental", UniqueKey: "k", Refs: []string{"a", "b"}}, ""},
		{"incremental needs key", "{{ config(materialized='incremental') }} SELECT 1", Model{}, "unique_key"},
		{"unknown materialization", "{{ config(materialized='ephemeral') }} SELECT 1", Model{}, "materialized"},
		{"unknown config key", "{{ config(materialized='table', schema='x') }} SELECT 1", Model{}, "schema"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := ParseModel("m", tc.text)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if m.Name != tc.want.Name || m.Materialized != tc.want.Materialized || m.UniqueKey != tc.want.UniqueKey || strings.Join(m.Refs, ",") != strings.Join(tc.want.Refs, ",") {
				t.Errorf("got %+v, want %+v", m, tc.want)
			}
		})
	}
}

func TestRender(t *testing.T) {
	ctx := RenderContext{Sources: map[string]string{"tr": "src"}, Vars: map[string]string{"tz": "Europe/Berlin"}}
	for _, tc := range []struct {
		name, in, want, err string
	}{
		{"all", "{{ config(materialized='table') }}SELECT '{{ var('tz') }}' FROM {{ source('tr', 'events') }} JOIN {{ ref('dim_host') }}",
			"SELECT 'Europe/Berlin' FROM src.events JOIN dim_host", ""},
		{"unknown var", "SELECT {{ var('nope') }}", "", "nope"},
		{"unknown source", "SELECT * FROM {{ source('x', 'y') }}", "", "source"},
		{"macro", "SELECT {{ dbt_utils.star('x') }}", "", "subset"},
		{"control flow", "{% if is_incremental() %}x{% endif %}", "", "subset"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Render(tc.in, ctx)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(got) != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadProjectOrdersByRef(t *testing.T) {
	fsys := fstest.MapFS{
		"models/a/c.sql": {Data: []byte("SELECT * FROM {{ ref('b') }}")},
		"models/b.sql":   {Data: []byte("SELECT * FROM {{ ref('a') }}")},
		"models/a.sql":   {Data: []byte("SELECT 1")},
		"models/x.yml":   {Data: []byte("ignored")},
	}
	p, err := LoadProject(fsys, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range p.Models {
		names = append(names, m.Name)
	}
	if got := strings.Join(names, ","); got != "a,b,c" {
		t.Errorf("order %s, want a,b,c", got)
	}
}

func TestLoadProjectErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		fs   fstest.MapFS
		err  string
	}{
		{"cycle", fstest.MapFS{"models/a.sql": {Data: []byte("SELECT * FROM {{ ref('b') }}")}, "models/b.sql": {Data: []byte("SELECT * FROM {{ ref('a') }}")}}, "cycle"},
		{"missing ref", fstest.MapFS{"models/a.sql": {Data: []byte("SELECT * FROM {{ ref('zz') }}")}}, "zz"},
		{"duplicate", fstest.MapFS{"models/a.sql": {Data: []byte("SELECT 1")}, "models/s/a.sql": {Data: []byte("SELECT 1")}}, "twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadProject(tc.fs, nil)
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("err = %v, want %q", err, tc.err)
			}
		})
	}
}

func TestLoadProjectPlacesSeeds(t *testing.T) {
	fsys := fstest.MapFS{
		"models/key.sql":   {Data: []byte("SELECT 1")},
		"models/named.sql": {Data: []byte("SELECT * FROM {{ ref('key') }} JOIN {{ ref('seed_names') }}")},
	}
	p, err := LoadProject(fsys, map[string][]string{"seed_names": {"key"}})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range p.Models {
		names = append(names, m.Name+":"+m.Materialized)
	}
	if got := strings.Join(names, ","); got != "key:view,seed_names:seed,named:view" {
		t.Errorf("order %s", got)
	}
}
