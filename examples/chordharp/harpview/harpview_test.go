package harpview

import (
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/extension"
)

func TestBlockCarriesTheGrid(t *testing.T) {
	block, err := Block("instrument")
	if err != nil {
		t.Fatal(err)
	}
	var c Content
	if err := json.Unmarshal([]byte(block.Payload), &c); err != nil {
		t.Fatal(err)
	}
	if got := [3]string{c.Chords[0][3], c.Chords[1][0], c.Chords[2][8]}; got != [3]string{"C", "Ebm", "B7"} {
		t.Fatalf("grid corners %v", got)
	}
	if c.Kinds[2] != "7th" || c.Patterns[0] != "Rock" {
		t.Fatalf("labels %v %v", c.Kinds, c.Patterns)
	}
}

func TestValidateRejectsOtherShapes(t *testing.T) {
	good, _ := Block("instrument")
	for name, component := range map[string]extension.Component{
		"navigates": {ID: good.ID, Name: Name, Payload: good.Payload, Destinations: map[string]string{"x": "/x"}},
		"short row": {ID: good.ID, Name: Name, Payload: `{"kinds":["a","b","c"],"chords":[["C"],["C"],["C"]],"patterns":["Rock"]}`},
		"bad id":    {ID: "has space", Name: Name, Payload: good.Payload},
	} {
		if validate(component) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestDefinitionParses(t *testing.T) {
	d := Definition()
	if d.Manifest.Name != Name || d.Manifest.Android == nil || d.Manifest.Android.Factory != "ChordHarpComponent" {
		t.Fatalf("manifest %+v", d.Manifest)
	}
	for _, source := range d.Manifest.Android.Sources {
		if _, err := assets.ReadFile(source); err != nil {
			t.Fatal(err)
		}
	}
}
