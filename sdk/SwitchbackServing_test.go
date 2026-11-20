package sdk

// Switchback attribute paths, cluster keys and source order, ported from the
// Java SDK's SwitchbackAttributePathTest, SwitchbackServingTest and
// SwitchbackServerParityTest (java-sdk main 07807b5).
//
// testAssets/switchback/attribute-path-cases.json is a byte-for-byte copy of
// absmartly/abs 9bd2b5ca35 collector/src/test/resources/switchback/
// attribute-path-cases.json (git blob 7b1d3fed59856fcbc296ed68a820b43359ae1932),
// vendored through java-sdk core-api/src/test/resources/switchback/.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"reflect"
	"testing"

	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// The vendored fixtures are the Collector's blobs: the SHA-256 values are
// the ones the Java SDK pins for its copies.
func TestSwitchbackFixtures_MatchTheCollectorBlobs(t *testing.T) {
	for file, want := range map[string]string{
		"testAssets/switchback/golden-vectors.json":       "66dda4bafddaa119b030beafff8dc56f5750a8387919ffbe0842c93525ef32b6",
		"testAssets/switchback/attribute-path-cases.json": "2ee7eb0053997b48b8992980ffe11fe3accb2db01471aa9a617420e1609c6518",
	} {
		var raw, err = os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var sum = sha256.Sum256(raw)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s: sha256 %s, want %s", file, got, want)
		}
	}
}

func sbResolvePath(t *testing.T, path *switchbackPath, attributes map[string]interface{}) interface{} {
	t.Helper()
	var root, found = attributes[path.rootKey()]
	if !found {
		return nil
	}
	var current = root
	for i := 1; i < len(path.keys) && current != nil; i++ {
		current = switchbackStep(current, path.keys[i], path.indices[i], path.isIndex[i])
	}
	return current
}

func TestSwitchbackPath_MatchesEveryPathCase(t *testing.T) {
	var raw, err = os.ReadFile("testAssets/switchback/attribute-path-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Attributes map[string]interface{} `json:"attributes"`
		Cases      []struct {
			Path      string      `json:"path"`
			Supported bool        `json:"supported"`
			Value     interface{} `json:"value"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	expectInt(t, 7, len(fixture.Cases))
	for _, c := range fixture.Cases {
		var path = parseSwitchbackPath(c.Path)
		if !c.Supported {
			if path != nil {
				t.Errorf("%q is supported", c.Path)
			}
			continue
		}
		if path == nil {
			t.Fatalf("%q is unsupported", c.Path)
		}
		if got := sbResolvePath(t, path, fixture.Attributes); !reflect.DeepEqual(c.Value, got) {
			t.Errorf("%q: %v, want %v", c.Path, got, c.Value)
		}
		// The context reads the same cluster key from the attribute.
		var key, _ = c.Value.(string)
		if got := path.resolveAttribute(fixture.Attributes[path.rootKey()]); got != key {
			t.Errorf("%q: key %q, want %q", c.Path, got, key)
		}
	}
}

func TestSwitchbackPath_QuotedSegmentsAreLiteralKeys(t *testing.T) {
	var attributes = map[string]interface{}{"geo": map[string]interface{}{"0": "zero", "a/b": "slash"}}
	expectValue(t, "zero", sbResolvePath(t, parseSwitchbackPath("geo/'0'"), attributes))
	expectValue(t, "slash", sbResolvePath(t, parseSwitchbackPath(`geo/"a/b"`), attributes))
	expectValue(t, "slash", parseSwitchbackPath(`geo/"a/b"`).resolveAttribute(attributes["geo"]))
}

// An unterminated quote anywhere, or a '.' anywhere, quoted or not, is
// malformed.
func TestSwitchbackPath_RejectsMalformedPaths(t *testing.T) {
	for _, path := range []string{"geo/'0", "'geo", `"geo/city`, `geo/"`, "geo.city", "geo/a.b", "a./b", ".", "items/0.5/city",
		"'geo.city'", `"geo.city"`, "geo/'a.b'"} {
		if parseSwitchbackPath(path) != nil {
			t.Errorf("%q parsed", path)
		}
	}
}

func TestSwitchbackPath_SeparatorsAreCollapsed(t *testing.T) {
	var attributes = map[string]interface{}{"geo": map[string]interface{}{"city": "Porto"}}
	expectValue(t, "Porto", sbResolvePath(t, parseSwitchbackPath("/geo//city/"), attributes))
	expectValue(t, attributes["geo"], sbResolvePath(t, parseSwitchbackPath("geo"), attributes))
}

func TestSwitchbackPath_RejectsUnsupportedPaths(t *testing.T) {
	for _, path := range []string{"", "/", "//", "0/city", "items/0", "items/0/1/city", "items/-1/city", "'"} {
		if parseSwitchbackPath(path) != nil {
			t.Errorf("%q parsed", path)
		}
	}
}

func TestSwitchbackPath_IndexSegmentsReadOnlyListsAndKeySegmentsReadOnlyMaps(t *testing.T) {
	var attributes = map[string]interface{}{
		"items": []interface{}{map[string]interface{}{"city": "Lisbon"}},
		"geo":   map[string]interface{}{"0": map[string]interface{}{"city": "zero"}},
		"name":  "scalar",
	}
	expectValue(t, nil, sbResolvePath(t, parseSwitchbackPath("geo/0/city"), attributes))
	expectValue(t, nil, sbResolvePath(t, parseSwitchbackPath("items/1/city"), attributes))
	expectValue(t, nil, sbResolvePath(t, parseSwitchbackPath("name/x"), attributes))
	expectValue(t, nil, sbResolvePath(t, parseSwitchbackPath("missing/x"), attributes))
	expectValue(t, "Lisbon", sbResolvePath(t, parseSwitchbackPath("items/-0/city"), attributes))
	// A list segment never reads a map key that looks like an index, nor a
	// key segment a list.
	expectValue(t, "", parseSwitchbackPath("geo/0/city").resolveAttribute(attributes["geo"]))
	expectValue(t, "", parseSwitchbackPath("items/city").resolveAttribute(attributes["items"]))
	// A root that is a scalar has nothing below it.
	expectValue(t, "", parseSwitchbackPath("name/x").resolveAttribute("scalar"))
	expectValue(t, "scalar", parseSwitchbackPath("name").resolveAttribute("scalar"))
}

func TestSwitchbackPath_TryParseIntFollowsGuava(t *testing.T) {
	for text, want := range map[string]int{"0": 0, "-0": 0, "007": 7, "2147483647": 2147483647, "-2147483648": -2147483648} {
		if got, ok := tryParseInt32(text); !ok || got != want {
			t.Errorf("%q: %d, %v", text, got, ok)
		}
	}
	for _, text := range []string{"", "-", "+1", "1a", " 1", "2147483648", "-2147483649", "99999999999999999999", "٠", "1.0"} {
		if _, ok := tryParseInt32(text); ok {
			t.Errorf("%q parsed", text)
		}
	}
}

// The cluster-value rule: a non-empty string as-is, a whole-number integer
// as its base-10 text; every float (3.0 included), bool, object, array, ""
// and nil give none.
func TestSwitchbackClusterKey_Rule(t *testing.T) {
	for value, want := range map[interface{}]string{
		"s": "s", " ": " ", 0: "0", -7: "-7", int8(-128): "-128", uint16(65535): "65535",
		int64(-9223372036854775808): "-9223372036854775808", uint64(18446744073709551615): "18446744073709551615",
		json.Number("42"): "42", json.Number("-0"): "0", json.Number("00042"): "",
		json.Number("3.0"): "", json.Number("3e0"): "", json.Number("-1.5"): "",
		3.0: "", float32(3): "", 1.5: "", true: "", false: "", "": "",
	} {
		if got := switchbackClusterKey(value); got != want {
			t.Errorf("%#v: %q, want %q", value, got, want)
		}
	}
	expectValue(t, "", switchbackClusterKey(nil))
	expectValue(t, "", switchbackClusterKey(map[string]interface{}{}))
	expectValue(t, "", switchbackClusterKey([]interface{}{}))
	expectValue(t, "123456789012345678901234567890", switchbackClusterKey(func() *big.Int {
		var value, _ = new(big.Int).SetString("123456789012345678901234567890", 10)
		return value
	}()))
}

// Values from a JSON document decoded with UseNumber keep 3 and 3.0 apart;
// decoded into float64 (the encoding/json default) every number is a float
// and gives no key, so callers that set attributes from JSON should decode
// with UseNumber, as the README says.
func TestSwitchbackClusterKey_JSONNumbersKeepIntegerAndFloatApart(t *testing.T) {
	var decoder = json.NewDecoder(bytes.NewReader([]byte(`{"int":3,"float":3.0,"exp":3e0,"big":4000000002}`)))
	decoder.UseNumber()
	var values map[string]interface{}
	if err := decoder.Decode(&values); err != nil {
		t.Fatal(err)
	}
	expectValue(t, "3", switchbackClusterKey(values["int"]))
	expectValue(t, "", switchbackClusterKey(values["float"]))
	expectValue(t, "", switchbackClusterKey(values["exp"]))
	expectValue(t, "4000000002", switchbackClusterKey(values["big"]))
}

// The declared source order alone decides which unit serves; the context
// keeps its units in a map.
func TestSwitchbackServing_SourceOrderDecidesWhateverTheUnitOrder(t *testing.T) {
	var definition = newSwitchbackDefinition(sbClusters("device_id", "device_city", "store_id", "store_city"))
	var values = map[string]string{"device_city": "Lisboa", "store_city": "Lisboa"}
	var read = func(path *switchbackPath) string { return values[path.rootKey()] }
	for i := 0; i < 20; i++ {
		var serving = resolveSwitchbackServing(definition, nil, map[string]string{"store_id": "s1", "device_id": "d1"}, read)
		expectValue(t, switchbackServing{status: switchbackEligible, unitType: "device_id", uid: "d1", clusterKey: "Lisboa"}, serving)
	}
	values["device_city"] = ""
	var serving = resolveSwitchbackServing(definition, nil, map[string]string{"store_id": "s1", "device_id": "d1"}, read)
	expectValue(t, "store_id", serving.unitType)
	values["device_city"] = "Porto"
	serving = resolveSwitchbackServing(definition, nil, map[string]string{"store_id": "s1", "device_id": "d1"}, read)
	expectValue(t, switchbackServing{status: switchbackIneligible, unitType: "device_id", uid: "d1"}, serving)
}

// Three or more sources whose declared order is not alphabetical: the first
// declared unit with a value serves, so a sorted or unordered map would pick
// another unit.
func TestSwitchbackServing_ManySourcesKeepTheDeclaredOrder(t *testing.T) {
	var payload = `{"experiments":[{"id":1,"name":"exp_sb","unitType":"user_id","iteration":1,"seedHi":1,"seedLo":2,
		"split":[0.5,0.5],"trafficSeedHi":1,"trafficSeedLo":2,"trafficSplit":[0,1],"variants":[{"name":"A"},{"name":"B"}],
		"randomizationType":"switchback","switchbackClusters":{"zone_id":"zone","alpha_id":"alpha","mid_id":"mid"},
		"switchbackStartAt":1767225600000,"switchbackPeriodMs":3600000}]}`
	var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	expectValue(t, sbClusters("zone_id", "zone", "alpha_id", "alpha", "mid_id", "mid"), data.Experiments[0].SwitchbackClusters)
	var h = sbCreateAt(t, sbAnchor+1, map[string]string{"alpha_id": "a1", "mid_id": "m1", "zone_id": "z1"}, data, sbNoClusterAttribute())
	h.setAttribute("alpha", "k")
	h.setAttribute("mid", "k")
	h.setAttribute("zone", "k")
	h.treatment("exp_sb")
	var exposures = h.publishExposures()
	expectInt(t, 1, len(exposures))
	expectValue(t, "zone_id", exposures[0].Unit)
}

func TestSwitchbackClusters_DecodeKeepsTheOrderAndToleratesMalformedValues(t *testing.T) {
	for raw, want := range map[string]jsonmodels.SwitchbackClusters{
		`{"b":"x","a":"y","c":"z"}`:  sbClusters("b", "x", "a", "y", "c", "z"),
		`{"b":"x","a":"y","b":"w"}`:  sbClusters("b", "w", "a", "y"),
		`{"a":1,"b":true}`:           sbClusters("a", "1", "b", "true"),
		`{}`:                         {},
		`null`:                       nil,
		`[]`:                         nil,
		`"a"`:                        nil,
		`{"a":null}`:                 nil,
		`{"a":{"b":"c"}}`:            nil,
		`{"a":["b"]}`:                nil,
		`{"a":"\u00e9\/\"q\""}`:      sbClusters("a", "é/\"q\""),
		`  {"a" : "b" , "c" : "d"} `: sbClusters("a", "b", "c", "d"),
	} {
		var clusters jsonmodels.SwitchbackClusters
		if err := json.Unmarshal([]byte(raw), &clusters); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if !reflect.DeepEqual(want, clusters) {
			t.Errorf("%s: %#v, want %#v", raw, clusters, want)
		}
	}
	var encoded, _ = json.Marshal(sbClusters("z", `a"b`, "a", "c"))
	expectValue(t, `{"z":"a\"b","a":"c"}`, string(encoded))
	encoded, _ = json.Marshal(jsonmodels.SwitchbackClusters(nil))
	expectValue(t, `null`, string(encoded))
}
