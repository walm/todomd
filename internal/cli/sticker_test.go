package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func jsonSticker(t *testing.T, path, id string) *string {
	t.Helper()
	out, err := run(t, "--file", path, "show", id, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var tk struct {
		Sticker *string `json:"sticker"`
	}
	if err := json.Unmarshal([]byte(out), &tk); err != nil || !strings.Contains(out, `"sticker":`) {
		t.Fatalf("%v\n%s", err, out)
	}
	return tk.Sticker
}

func TestStickerCommands(t *testing.T) {
	path := testFile(t)
	a := addOne(t, path, "Hot", "--sticker", "🔥")
	b := addOne(t, path, "Ribbon", "--sticker", "Red")
	c := addOne(t, path, "Plain")
	if s := jsonSticker(t, path, a); s == nil || *s != "fire" {
		t.Errorf("a: %v", s)
	}
	if s := jsonSticker(t, path, c); s != nil {
		t.Errorf("c: %v", *s)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "\n\n<!-- sticker: fire -->\n") || !strings.Contains(string(data), "\n\n<!-- sticker: red -->\n") {
		t.Errorf("file:\n%s", data)
	}

	out, err := run(t, "--file", path, "sticker", c, "❤")
	if err != nil || !strings.Contains(out, "sticker ❤️ heart on "+c) {
		t.Fatalf("%v %s", err, out)
	}
	if out, _ := run(t, "--file", path, "sticker", c); strings.TrimSpace(out) != "❤️ heart" {
		t.Errorf("print: %q", out)
	}
	if out, err := run(t, "--file", path, "sticker", c, "none"); err != nil || !strings.Contains(out, "removed the sticker") {
		t.Errorf("none: %v %s", err, out)
	}
	if s := jsonSticker(t, path, c); s != nil {
		t.Errorf("c after none: %v", *s)
	}
	if _, err := run(t, "--file", path, "sticker", c, "banana"); err == nil || !strings.Contains(err.Error(), "unknown sticker") {
		t.Errorf("banana: %v", err)
	}
	if _, err := run(t, "--file", path, "add", "x", "--sticker", "banana"); err == nil {
		t.Error("add --sticker banana accepted")
	}
	if _, err := run(t, "--file", path, "update", b, "--sticker", "violet"); err != nil {
		t.Fatal(err)
	}
	if s := jsonSticker(t, path, b); s == nil || *s != "violet" {
		t.Errorf("b: %v", s)
	}

	ids := func(args ...string) string {
		out, err := run(t, append([]string{"--file", path, "list", "--json"}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		var l struct {
			Boards []struct {
				Tasks []struct {
					ID string `json:"id"`
				} `json:"tasks"`
			} `json:"boards"`
		}
		json.Unmarshal([]byte(out), &l)
		var got []string
		for _, bd := range l.Boards {
			for _, x := range bd.Tasks {
				got = append(got, x.ID)
			}
		}
		return strings.Join(got, ",")
	}
	if got := ids("--sticker", "any"); got != a+","+b {
		t.Errorf("any: %s", got)
	}
	if got := ids("--sticker", "none"); got != c {
		t.Errorf("none: %s", got)
	}
	if got := ids("--sticker", "🔥"); got != a {
		t.Errorf("fire: %s", got)
	}
	if _, err := run(t, "--file", path, "list", "--sticker", "banana"); err == nil {
		t.Error("list --sticker banana accepted")
	}

	// The text list shows the sticker in a last column.
	out, _ = run(t, "--file", path, "list")
	if !strings.Contains(out, "🔥") || !strings.Contains(out, "◥ violet") {
		t.Errorf("list:\n%s", out)
	}
	out, _ = run(t, "--file", path, "show", a)
	if !strings.Contains(out, "sticker: 🔥 fire") {
		t.Errorf("show:\n%s", out)
	}
	out, _ = run(t, "sticker", "--json")
	var all []map[string]string
	if json.Unmarshal([]byte(out), &all); len(all) != 17 || all[0]["name"] != "fire" || all[16]["kind"] != "ribbon" {
		t.Errorf("sticker --json: %s", out)
	}
}
