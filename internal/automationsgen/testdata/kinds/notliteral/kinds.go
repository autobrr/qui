package notliteral

type Kind string

const prefix = "pre."

const (
	KindAlpha Kind = "alpha"
	KindBravo Kind = prefix + "bravo"
)
