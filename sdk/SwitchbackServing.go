package sdk

import (
	"bytes"
	"encoding"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// switchbackPath is a switchback cluster attribute path, resolved exactly
// like the Collector's SwitchbackAttributePath (Java SDK:
// internal/SwitchbackAttributePath): "/" separates segments, a segment
// wrapped in ' or " is a literal key, and an unquoted segment that Guava
// Ints.tryParse accepts (ASCII digits, optional leading '-') indexes a list
// and nothing else. Every other segment reads a map key. A path is
// malformed, and its experiment unservable, when a quote is left
// unterminated or any segment key, quoted or not, contains a '.' (dotted
// paths are not a nesting syntax; nesting is written with '/').
type switchbackPath struct {
	keys    []string
	indices []int
	isIndex []bool
}

// switchbackMaxDepth bounds the containers an attribute value may nest, as
// the Java SDK does before encoding it.
const switchbackMaxDepth = 64

// parseSwitchbackPath returns nil for an unsupported path: malformed (see
// switchbackPath), empty, or an array index that is negative, first, last,
// or directly after another index. Analysis cannot read those.
func parseSwitchbackPath(path string) *switchbackPath {
	if strings.IndexByte(path, '.') >= 0 {
		return nil
	}
	var parsed = &switchbackPath{}
	for i := 0; i < len(path); {
		if path[i] == '/' {
			i++
			continue
		}
		if quote := path[i]; quote == '\'' || quote == '"' {
			var end = strings.IndexByte(path[i+1:], quote)
			if end < 0 {
				return nil
			}
			parsed.add(path[i+1:i+1+end], 0, false)
			i += end + 2
			continue
		}
		var stop = strings.IndexByte(path[i:], '/')
		if stop < 0 {
			stop = len(path)
		} else {
			stop += i
		}
		var key = path[i:stop]
		var index, isIndex = tryParseInt32(key)
		parsed.add(key, index, isIndex)
		i = stop
	}
	var n = len(parsed.keys)
	if n == 0 {
		return nil
	}
	for i := 0; i < n; i++ {
		if parsed.isIndex[i] && (parsed.indices[i] < 0 || i == 0 || i == n-1 || parsed.isIndex[i-1]) {
			return nil
		}
	}
	return parsed
}

func (p *switchbackPath) add(key string, index int, isIndex bool) {
	p.keys = append(p.keys, key)
	p.indices = append(p.indices, index)
	p.isIndex = append(p.isIndex, isIndex)
}

// rootKey is the attribute name the path starts at; a supported path always
// starts at a key segment.
func (p *switchbackPath) rootKey() string {
	return p.keys[0]
}

// tryParseInt32 follows Guava Ints.tryParse: an optional '-', then one or
// more ASCII digits, within int32 range. "+1" and non-ASCII digits are not
// numbers; "-0" is 0.
func tryParseInt32(s string) (int, bool) {
	var digits = strings.TrimPrefix(s, "-")
	if len(digits) == 0 || digits[0] < '0' || digits[0] > '9' {
		return 0, false
	}
	var value, err = strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, false
	}
	return int(value), true
}

// resolveAttribute reads the path below rootKey in an attribute value, as the
// Collector reads the value's JSON encoding, and returns the cluster key it
// yields (see switchbackClusterKey), or "" for none. A value with no JSON
// encoding, or one that contains a container cycle or nests containers
// deeper than switchbackMaxDepth, yields none.
//
// Maps, slices and scalars are walked in place rather than re-encoded, so a
// Go float stays a float: encoding/json writes 3.0 as 3, which would read
// back as an integer. Values with their own JSON encoding (a json.Marshaler
// or encoding.TextMarshaler, []byte, structs, maps with non-string keys) are
// read from that encoding, decoded with UseNumber. It runs under ContextLock_,
// so such a marshaler must not call back into the context.
func (p *switchbackPath) resolveAttribute(root interface{}) string {
	if root == nil {
		return ""
	}
	if !switchbackEncodable(reflect.ValueOf(root), map[switchbackContainer]bool{}, 0) {
		return ""
	}
	var current = root
	for i := 1; i < len(p.keys) && current != nil; i++ {
		current = switchbackStep(current, p.keys[i], p.indices[i], p.isIndex[i])
	}
	return switchbackClusterKey(current)
}

// switchbackStep reads one segment: an index segment reads a list, a key
// segment reads a map with string keys; anything else gives nil.
func switchbackStep(current interface{}, key string, index int, isIndex bool) interface{} {
	var value = switchbackJSONView(reflect.ValueOf(current))
	if !value.IsValid() {
		return nil
	}
	if isIndex {
		if (value.Kind() == reflect.Slice || value.Kind() == reflect.Array) && index < value.Len() {
			return jsonElement(value.Index(index)).Interface()
		}
		return nil
	}
	if value.Kind() == reflect.Map && value.Type().Key().Kind() == reflect.String {
		var element = value.MapIndex(reflect.ValueOf(key).Convert(value.Type().Key()))
		if element.IsValid() && utf8.ValidString(key) && !strings.ContainsRune(key, utf8.RuneError) {
			return element.Interface()
		}
		// Ambiguous published names fail closed, independent of map order.
		var match reflect.Value
		var it = value.MapRange()
		for it.Next() {
			if publishedString(it.Key().String()) == key {
				if match.IsValid() {
					return nil
				}
				match = it.Value()
			}
		}
		if match.IsValid() {
			return match.Interface()
		}
	}
	return nil
}

// jsonElement is a container element as encoding/json encodes it: an
// addressable element (a slice's) whose pointer type has a JSON or text
// marshaler is encoded through that marshaler, so the element is read
// through its address.
func jsonElement(element reflect.Value) reflect.Value {
	if element.Kind() != reflect.Ptr && element.Kind() != reflect.Interface && element.CanAddr() {
		var pointer = element.Addr()
		if element.Kind() == reflect.Struct || pointer.Type().Implements(jsonMarshalerType) || pointer.Type().Implements(textMarshalerType) {
			return pointer
		}
	}
	return element
}

// switchbackJSONView is the value as a JSON reader would see it: pointers
// and interfaces dereferenced (nil gives an invalid value), and values whose
// encoding is not their Go shape replaced by that encoding decoded with
// UseNumber.
func switchbackJSONView(value reflect.Value) reflect.Value {
	for hops := 0; value.IsValid() && (value.Kind() == reflect.Ptr || value.Kind() == reflect.Interface); hops++ {
		if value.IsNil() || hops > switchbackMaxDepth {
			return reflect.Value{}
		}
		if switchbackOpaque(value) {
			break
		}
		value = value.Elem()
	}
	if !value.IsValid() || !switchbackOpaque(value) {
		return value
	}
	var encoded, err = json.Marshal(jsonElement(value).Interface())
	if err != nil {
		return reflect.Value{}
	}
	var decoder = json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var decoded interface{}
	if decoder.Decode(&decoded) != nil || decoded == nil {
		return reflect.Value{}
	}
	return reflect.ValueOf(decoded)
}

var (
	jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	jsonNumberType    = reflect.TypeOf(json.Number(""))
)

// switchbackOpaque reports whether a value's JSON encoding is not its Go
// shape, so it has to be read from that encoding.
func switchbackOpaque(value reflect.Value) bool {
	var t = value.Type()
	if t == jsonNumberType {
		return false
	}
	if t.Implements(jsonMarshalerType) || t.Implements(textMarshalerType) {
		return true
	}
	switch value.Kind() {
	case reflect.Struct:
		return true
	case reflect.Slice:
		return t.Elem().Kind() == reflect.Uint8
	case reflect.Map:
		return t.Key().Kind() != reflect.String
	}
	return false
}

// switchbackEncodable reports whether the value has a JSON encoding without
// a container cycle and with containers nested at most switchbackMaxDepth
// deep. Elements are checked as encoding/json encodes them (jsonElement).
// path holds the containers on the current branch only, so a value shared by
// two siblings is not a cycle.
type switchbackContainer struct {
	pointer uintptr
	length  int
}

func switchbackEncodable(value reflect.Value, path map[switchbackContainer]bool, depth int) bool {
	for hops := 0; value.IsValid() && (value.Kind() == reflect.Ptr || value.Kind() == reflect.Interface) && !switchbackOpaque(value); hops++ {
		if value.IsNil() {
			return true
		}
		if hops > switchbackMaxDepth {
			// A pointer or interface cycle (var v any; v = &v) has no
			// JSON encoding.
			return false
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return true
	}
	if switchbackOpaque(value) || value.Type() == jsonNumberType {
		var _, err = json.Marshal(jsonElement(value).Interface())
		return err == nil
	}
	switch value.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
	case reflect.Chan, reflect.Func, reflect.UnsafePointer, reflect.Complex64, reflect.Complex128:
		return false
	case reflect.Float32, reflect.Float64:
		// NaN and the infinities have no JSON encoding.
		var _, err = json.Marshal(jsonElement(value).Interface())
		return err == nil
	default:
		return true
	}
	if depth >= switchbackMaxDepth {
		return false
	}
	var container switchbackContainer
	if value.Kind() != reflect.Array {
		if value.IsNil() {
			return true
		}
		container.pointer = value.Pointer()
		if value.Kind() == reflect.Slice {
			container.length = value.Len()
		}
		if path[container] {
			return false
		}
		path[container] = true
		defer delete(path, container)
	}
	if value.Kind() == reflect.Map {
		var it = value.MapRange()
		for it.Next() {
			if !switchbackEncodable(it.Value(), path, depth+1) {
				return false
			}
		}
		return true
	}
	for i := 0; i < value.Len(); i++ {
		if !switchbackEncodable(jsonElement(value.Index(i)), path, depth+1) {
			return false
		}
	}
	return true
}

// switchbackClusterKey is the Collector's SwitchbackClusterKeys.normalize: a
// non-empty string is the key as-is, and a whole-number integer is its
// base-10 text. Every floating-point number (3.0 included), boolean, map,
// list, empty string and nil gives no key (""). A json.Number is an integer
// only when written without a fraction or exponent. The key is passed to the
// assigner unhashed.
func switchbackClusterKey(value interface{}) string {
	var v = switchbackJSONView(reflect.ValueOf(value))
	if !v.IsValid() {
		return ""
	}
	if v.Type() == jsonNumberType {
		if v.String() == "" {
			return "0"
		}
		return integerNumberText(v.String())
	}
	switch v.Kind() {
	case reflect.String:
		return publishedString(v.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(v.Uint(), 10)
	}
	return ""
}

// publishedString is s as encoding/json publishes it: each byte of an
// invalid UTF-8 sequence becomes U+FFFD.
func publishedString(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.Map(func(r rune) rune { return r }, s)
}

// integerNumberText is the decimal text of a JSON number written as an
// integer, as Jackson reads it into an Integer, Long or BigInteger ("-0" is
// "0"), or "" for a number with a fraction or an exponent, which Jackson
// reads as a Double, and for text that is not a JSON integer.
func integerNumberText(number string) string {
	var digits = strings.TrimPrefix(number, "-")
	if len(digits) == 0 || (digits[0] == '0' && len(digits) > 1) || strings.Trim(digits, "0123456789") != "" {
		return ""
	}
	if number == "-0" {
		return "0"
	}
	return number
}

// Serving statuses of a switchback experiment, as the Collector's
// ContextAPI.switchbackServing reports them (Java SDK:
// internal/SwitchbackServing).
const (
	// switchbackAbsent: no context unit of a linked type; the experiment
	// does not apply.
	switchbackAbsent = iota
	// switchbackIneligible: no unit yields a value, units disagree, or the
	// value is not allowed. Served under the first linked unit.
	switchbackIneligible
	// switchbackEligible: the serving unit and its raw cluster key.
	switchbackEligible
)

// switchbackServing is which unit serves a switchback experiment and under
// which cluster key.
type switchbackServing struct {
	status int
	// unitType and uid are the serving unit when eligible, the first linked
	// unit when ineligible, and empty when absent.
	unitType string
	uid      string
	// clusterKey is the raw cluster value when eligible; never hashed.
	clusterKey string
}

// switchbackDefinition is what setData derives from a switchback experiment
// once, so a resolution needs no parsing.
type switchbackDefinition struct {
	// sources are the switchbackClusters entries in declared order, each
	// path parsed. sources is nil when the map is missing, empty or holds an
	// unsupported path: the legacy path.
	sources []switchbackSource
	// generation is carried over by a refresh while the switchback identity
	// is unchanged, and fresh otherwise; cached slots of another generation
	// are stale.
	generation uint64
}

type switchbackSource struct {
	unitType string
	path     *switchbackPath
}

func newSwitchbackDefinition(clusters jsonmodels.SwitchbackClusters) *switchbackDefinition {
	var definition = &switchbackDefinition{}
	var sources = make([]switchbackSource, 0, len(clusters))
	for _, cluster := range clusters {
		var path = parseSwitchbackPath(cluster.Path)
		if path == nil {
			return definition
		}
		sources = append(sources, switchbackSource{unitType: cluster.UnitType, path: path})
	}
	if len(sources) > 0 {
		definition.sources = sources
	}
	return definition
}

// resolveSwitchbackServing walks the sources in declared order. For each one
// the unit of that type, if set, reads the attribute at the source's path.
// The first unit that yields a value serves and its value is the cluster
// key; a unit yielding no value is skipped; a later unit with a different
// value is a disagreement. read returns the cluster key a path yields, or ""
// for none.
func resolveSwitchbackServing(definition *switchbackDefinition, allowed []string, units map[string]string,
	read func(*switchbackPath) string) switchbackServing {
	var first, serving switchbackServing
	var disagreement = false
	for _, source := range definition.sources {
		var unitType = source.unitType
		var uid, set = units[unitType]
		if !set {
			continue
		}
		if first.status == switchbackAbsent {
			first = switchbackServing{status: switchbackIneligible, unitType: unitType, uid: uid}
		}
		var value = read(source.path)
		if len(value) == 0 {
			continue
		}
		if len(serving.clusterKey) == 0 {
			serving = switchbackServing{status: switchbackEligible, unitType: unitType, uid: uid, clusterKey: value}
		} else if value != serving.clusterKey {
			disagreement = true
		}
	}
	if first.status == switchbackAbsent {
		return first
	}
	if len(serving.clusterKey) == 0 || disagreement || !switchbackValueAllowed(allowed, serving.clusterKey) {
		return first
	}
	return serving
}

// legacySwitchbackServing is the Collector's compatibility path for a
// missing, empty or unreadable cluster map: the declared unitType only
// identifies an ineligible control (never an assignment or a cluster), and
// without a unit of that type the experiment does not apply.
func legacySwitchbackServing(unitType string, units map[string]string) switchbackServing {
	if uid, set := units[unitType]; set && len(unitType) > 0 {
		return switchbackServing{status: switchbackIneligible, unitType: unitType, uid: uid}
	}
	return switchbackServing{status: switchbackAbsent}
}

// switchbackValueAllowed: a nil or empty allow-list admits every value, as
// the Collector builds its filter only for a non-empty list.
func switchbackValueAllowed(allowed []string, key string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, value := range allowed {
		if value == key {
			return true
		}
	}
	return false
}
