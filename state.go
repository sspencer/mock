package mock

type parseState int

const (
	stateNone parseState = iota
	stateBody
)

func (s parseState) String() string {
	switch s {
	case stateNone:
		return "None"
	case stateBody:
		return "Body"
	default:
		return "????"
	}
}
