//go:build jigtest

package e2e

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/client/llm/jigtest"
)

// writePNG writes a w×h PNG to path.
func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, x%h, color.RGBA{R: 200, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, buf.String())
}

func TestE2E_ReadImageReachesModel(t *testing.T) {
	one, zero := 1, 0
	cases := []struct {
		name   string
		model  string
		second jigtest.Turn
	}{
		{"image model", "m1", jigtest.Turn{ExpectMedia: &one, Text: "saw it"}},
		{"text-only model", "m2", jigtest.Turn{
			ExpectMedia:          &zero,
			ExpectPromptContains: []string{"[image omitted: jigtest/m2 does not accept images]"},
			Text:                 "saw it",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newEnv(t)
			writePNG(t, filepath.Join(env.work, "shot.png"), 20, 10)
			script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{
				c.model: {
					{Calls: []jigtest.Call{call("r1", "read", `{"path":"shot.png"}`)}},
					c.second,
				},
			}})
			writeConfig(t, env, jigtestConfig(script, ""))

			stdout, stderr, code := runPrompt(t, env, "--model", "jigtest/"+c.model, "look at shot.png")
			wantCode(t, code, 0, stdout, stderr)
			if !strings.Contains(stdout, "saw it") {
				t.Errorf("stdout = %q, want %q\nstderr:\n%s", stdout, "saw it", stderr)
			}
		})
	}
}

func TestE2E_ImageAttachment(t *testing.T) {
	env := newEnv(t)
	writePNG(t, filepath.Join(env.work, "shot.png"), 20, 10)
	writeFile(t, filepath.Join(env.work, "notes.txt"), "notes content")
	one := 1
	script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{
		"m1": {
			{ExpectMedia: &one, ExpectPromptContains: []string{`<attachment path=`, "notes content"}, Text: "got it"},
		},
	}})
	writeConfig(t, env, jigtestConfig(script, ""))

	stdout, stderr, code := runPrompt(t, env, "--attach", "shot.png", "--attach", "notes.txt", "look")
	wantCode(t, code, 0, stdout, stderr)
	if !strings.Contains(stdout, "got it") {
		t.Errorf("stdout = %q, want %q\nstderr:\n%s", stdout, "got it", stderr)
	}
}

func TestE2E_BinaryAttachmentExits2(t *testing.T) {
	env := newEnv(t)
	writeFile(t, filepath.Join(env.work, "blob.dat"), "start\x00end")
	script := writeScript(t, env, "script.json", jigtest.Script{Models: map[string][]jigtest.Turn{"m1": {}}})
	writeConfig(t, env, jigtestConfig(script, ""))

	stdout, stderr, code := runPrompt(t, env, "--attach", "blob.dat", "look")
	wantCode(t, code, 2, stdout, stderr)
	if !strings.Contains(stderr, "blob.dat") {
		t.Errorf("stderr = %q, want it to name blob.dat", stderr)
	}
}
