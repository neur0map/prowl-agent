package context

import "encoding/json"

// CanonicalProjection returns the transport-independent packet representation.
// TraceID identifies one execution and is deliberately absent so equal project
// requests compare by their durable evidence, selection, and budget fields.
func CanonicalProjection(packet Packet) Packet {
	packet.TraceID = ""
	return packet
}

// MarshalCanonicalProjection emits the stable JSON form shared by CLI, MCP,
// and the workbench after volatile transport and execution fields are removed.
func MarshalCanonicalProjection(packet Packet) ([]byte, error) {
	return json.Marshal(CanonicalProjection(packet))
}
