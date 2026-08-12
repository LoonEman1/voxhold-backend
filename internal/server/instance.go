package server

const ProtocolVersion = 1

type Instance struct {
	ID          string
	Name        string
	Initialized bool
	CreatedAt   int64
}
