package core

import (
	"encoding/json"
	"testing"
)

// TestMedia_DataNeverMarshaled pins R4: Media.Data is json:"-" so it is
// never persisted, only filled by client/llm just before conversion.
func TestMedia_DataNeverMarshaled(t *testing.T) {
	m := Media{MIME: "image/png", Ref: "abc123", Data: []byte{1, 2, 3}}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := got["Data"]; ok {
		t.Errorf("Marshal(%+v) = %s, Data key present, want omitted", m, b)
	}
	if got["MIME"] != "image/png" || got["Ref"] != "abc123" {
		t.Errorf("Marshal(%+v) = %s, want MIME/Ref present", m, b)
	}
}

func TestAttachment_Fields(t *testing.T) {
	a := Attachment{Path: "/w/a.go", Content: "package a"}
	if a.Path != "/w/a.go" || a.Content != "package a" || a.Media != nil {
		t.Errorf("Attachment = %+v, want Path/Content set and Media nil", a)
	}

	media := &Media{MIME: "image/png", Ref: "abc123"}
	img := Attachment{Path: "/w/s.png", Media: media}
	if img.Media != media || img.Content != "" {
		t.Errorf("Attachment = %+v, want Media set and Content empty", img)
	}
}

func TestImageInfo_Fields(t *testing.T) {
	i := ImageInfo{Width: 100, Height: 200, Bytes: 300}
	if i.Width != 100 || i.Height != 200 || i.Bytes != 300 {
		t.Errorf("ImageInfo = %+v, want {100 200 300}", i)
	}
}
