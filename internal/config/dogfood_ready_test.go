package config

import "testing"

func TestDogfoodReadyValidation(t *testing.T) {
	if kind, ok := FieldKind("dogfood_ready"); !ok || kind != FieldKindShared {
		t.Fatalf("kind=%s ok=%v", kind, ok)
	}
	for _, block := range []*DogfoodReadyBlock{
		{Ports: []int{0, 65536}}, {HealthURLs: []string{"file:///tmp/no", "http://"}}, {TimeoutSeconds: -1}, {IntervalSeconds: -1},
	} {
		if errs := block.validate(); len(errs) == 0 {
			t.Fatalf("accepted %+v", block)
		}
	}
	if errs := (&DogfoodReadyBlock{Ports: []int{3333}, HealthURLs: []string{"http://localhost:3333/health"}}).validate(); len(errs) > 0 {
		t.Fatal(errs)
	}
}

func TestDogfoodReadyDecodeAndValidate(t *testing.T) {
	data := []byte("schema_version=1\n[build]\ncommand=[\"true\"]\n[restart]\nmechanism=\"none\"\n[dogfood_ready]\nports=[3333]\nhealth_urls=[\"http://localhost:3333/health\"]\ntimeout_seconds=5\ninterval_seconds=1\n")
	doc, errs, err := DecodeIrisToml(data, ".iris.toml", false)
	if err != nil || len(errs) > 0 || doc == nil || doc.DogfoodReady == nil || doc.DogfoodReady.IntervalSeconds != 1 || doc.DogfoodReady.TimeoutSeconds != 5 {
		t.Fatalf("doc=%+v errors=%v err=%v", doc, errs, err)
	}
}
