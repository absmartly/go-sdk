package main

type AudienceDeserializer interface {
	Deserialize(bytes []byte, offset int, length int) (map[string]interface{}, error)
}
