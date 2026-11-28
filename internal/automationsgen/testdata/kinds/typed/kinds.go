package typed

type Kind string

type Other string

const (
	KindZulu   Kind  = "zulu"
	KindAlpha  Kind  = "alpha"
	OtherTwo   Other = "two"
	kindHidden Kind  = "hidden"
)

const OtherOne Other = "other"

const KindMike Kind = "mike"
