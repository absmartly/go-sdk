package jsonmodels

import (
	"bytes"
	"encoding/json"
)

// SwitchbackCluster is one switchbackClusters entry: a linked unit type and
// the attribute path holding its cluster value.
type SwitchbackCluster struct {
	UnitType string
	Path     string
}

// SwitchbackClusters is a switchback experiment's switchbackClusters object
// in its JSON order. The order is semantic (the first linked unit with a
// value serves the experiment), so it is decoded into a slice rather than a
// Go map, which would lose it.
type SwitchbackClusters []SwitchbackCluster

// UnmarshalJSON decodes the object entries in order. A repeated key keeps
// its first position and its last value, as a JSON object read into an
// insertion-ordered map does. A number or boolean path is its JSON text, as
// the Java SDK coerces it. Anything but an object of scalar paths gives nil:
// the experiment then takes the legacy path for an unreadable map. It never
// fails, so a malformed value cannot fail the context payload.
func (s *SwitchbackClusters) UnmarshalJSON(data []byte) error {
	*s = decodeSwitchbackClusters(data)
	return nil
}

func decodeSwitchbackClusters(data []byte) SwitchbackClusters {
	var decoder = json.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	var clusters = SwitchbackClusters{}
	for decoder.More() {
		var key, err = decoder.Token()
		if err != nil {
			return nil
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			return nil
		}
		var path string
		switch {
		case len(raw) == 0 || raw[0] == '{' || raw[0] == '[' || bytes.Equal(raw, []byte("null")):
			return nil
		case raw[0] == '"':
			if json.Unmarshal(raw, &path) != nil {
				return nil
			}
		default:
			path = string(raw)
		}
		var unitType = key.(string)
		var found = false
		for i := range clusters {
			if clusters[i].UnitType == unitType {
				clusters[i].Path = path
				found = true
				break
			}
		}
		if !found {
			clusters = append(clusters, SwitchbackCluster{UnitType: unitType, Path: path})
		}
	}
	return clusters
}

// MarshalJSON encodes the entries as a JSON object in their order.
func (s SwitchbackClusters) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte("null"), nil
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, cluster := range s {
		if i > 0 {
			buf.WriteByte(',')
		}
		var key, err = json.Marshal(cluster.UnitType)
		if err != nil {
			return nil, err
		}
		var path []byte
		if path, err = json.Marshal(cluster.Path); err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(path)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
