package manifest

import (
	"strings"
	"testing"
)

func TestLoadEmbeddedManifest(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if m.ManifestVersion != 1 {
		t.Errorf("ManifestVersion = %d, want 1", m.ManifestVersion)
	}
	if m.GeneratedAt == "" || m.Source == "" {
		t.Errorf("GeneratedAt/Source must be set: %q / %q", m.GeneratedAt, m.Source)
	}
	if len(m.Endpoints) != 4 {
		t.Fatalf("len(Endpoints) = %d, want 4", len(m.Endpoints))
	}

	first := m.Endpoints[0]
	if got := strings.Join(first.Command, " "); got != "identity show" {
		t.Errorf("first command = %q, want %q", got, "identity show")
	}
	if first.Method != "GET" || first.Path != "/my/identity" {
		t.Errorf("first endpoint = %s %s", first.Method, first.Path)
	}

	update := m.Endpoints[3]
	if got := strings.Join(update.Command, " "); got != "settings email-message update" {
		t.Errorf("update command = %q", got)
	}
	if update.Method != "PATCH" || update.BodyRoot != "setting_email_message" {
		t.Errorf("update endpoint = %s body_root=%q", update.Method, update.BodyRoot)
	}
	if len(update.PathParams) != 1 || update.PathParams[0] != "template" {
		t.Errorf("update path_params = %v", update.PathParams)
	}
	if len(update.Params) != 1 || update.Params[0].Name != "content" || !update.Params[0].Required || update.Params[0].In != InBody {
		t.Errorf("update params = %+v", update.Params)
	}
}

func TestParseInvalidJSON(t *testing.T) {
	if _, err := Parse([]byte("{nope")); err == nil {
		t.Fatal("expected parse error for invalid JSON")
	}
}

func TestParseValidationErrors(t *testing.T) {
	cases := map[string]struct {
		body string
		want string
	}{
		"missing command": {
			body: `{"manifest_version":1,"endpoints":[{"method":"GET","path":"/x"}]}`,
			want: "missing command",
		},
		"empty command element": {
			body: `{"manifest_version":1,"endpoints":[{"command":["a",""],"method":"GET","path":"/x"}]}`,
			want: "empty command element",
		},
		"missing method": {
			body: `{"manifest_version":1,"endpoints":[{"command":["a"],"path":"/x"}]}`,
			want: "missing method",
		},
		"missing path": {
			body: `{"manifest_version":1,"endpoints":[{"command":["a"],"method":"GET"}]}`,
			want: "missing path",
		},
		"unknown param type": {
			body: `{"manifest_version":1,"endpoints":[{"command":["a"],"method":"GET","path":"/x",
				"params":[{"name":"p","type":"uuid","in":"query"}]}]}`,
			want: `unknown type "uuid"`,
		},
		"unknown param location": {
			body: `{"manifest_version":1,"endpoints":[{"command":["a"],"method":"GET","path":"/x",
				"params":[{"name":"p","type":"string","in":"header"}]}]}`,
			want: `unknown location "header"`,
		},
		"unknown array item type": {
			body: `{"manifest_version":1,"endpoints":[{"command":["a"],"method":"GET","path":"/x",
				"params":[{"name":"p","type":"array","item":"uuid","in":"query"}]}]}`,
			want: `unknown item type "uuid"`,
		},
		"path_param not in path": {
			body: `{"manifest_version":1,"endpoints":[{"command":["a"],"method":"GET","path":"/x/:id",
				"path_params":["id","other"]}]}`,
			want: `path param "other" not present in path`,
		},
		"path segment not declared": {
			body: `{"manifest_version":1,"endpoints":[{"command":["a"],"method":"GET","path":"/x/:id",
				"path_params":[]}]}`,
			want: `path segment ":id" not declared in path_params`,
		},
		"duplicate path_params": {
			body: `{"manifest_version":1,"endpoints":[{"command":["a"],"method":"GET","path":"/x/:id/:id",
				"path_params":["id","id"]}]}`,
			want: `duplicate path param "id"`,
		},
		"duplicate param name": {
			body: `{"manifest_version":1,"endpoints":[{"command":["a"],"method":"GET","path":"/x",
				"params":[{"name":"q","type":"string","in":"query"},{"name":"q","type":"integer","in":"query"}]}]}`,
			want: `duplicate param "q"`,
		},
		"missing param name": {
			body: `{"manifest_version":1,"endpoints":[{"command":["a"],"method":"GET","path":"/x",
				"params":[{"type":"string","in":"query"}]}]}`,
			want: "missing name",
		},
		"reserved param name format": {
			body: `{"manifest_version":1,"endpoints":[{"command":["a"],"method":"GET","path":"/x",
				"params":[{"name":"format","type":"string","in":"query"}]}]}`,
			want: `reserved name`,
		},
		"reserved param name help": {
			body: `{"manifest_version":1,"endpoints":[{"command":["a"],"method":"GET","path":"/x",
				"params":[{"name":"help","type":"string","in":"query"}]}]}`,
			want: `reserved name`,
		},
		"reserved param name h": {
			body: `{"manifest_version":1,"endpoints":[{"command":["a"],"method":"GET","path":"/x",
				"params":[{"name":"h","type":"string","in":"query"}]}]}`,
			want: `reserved name`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(tc.body))
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestParseValidMinimal(t *testing.T) {
	body := `{"manifest_version":1,"generated_at":"2026-01-01T00:00:00Z","source":"x@y","endpoints":[
		{"command":["thing","list"],"method":"GET","path":"/things","path_params":[],
		 "params":[{"name":"q","type":"string","in":"query"}]}]}`
	m, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if len(m.Endpoints) != 1 || m.Endpoints[0].Command[0] != "thing" {
		t.Errorf("manifest = %+v", m)
	}
}
