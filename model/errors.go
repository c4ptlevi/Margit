package model

type Error int

const (
	ErrNotFound Error = iota + 1
	ErrInvalidIdentifier
	ErrEmptyID
	ErrRelationNameMismatch
	ErrInvalidRelationKind
	ErrDuplicateType
	ErrInvalidExpression
	ErrUnknownNamespace
	ErrUnknownRelation
	ErrInvalidTupleset
	ErrCyclicRelation
	ErrNamespaceMismatch
	ErrRelationNotDirect
	ErrSubjectTypeNotAllowed
	ErrMaxDepthExceeded
	ErrNamespaceInUse
	ErrInvalidLimit
)

var errorText = map[Error]string{
	ErrNotFound:              "not found",
	ErrInvalidIdentifier:     "invalid identifier",
	ErrEmptyID:               "empty entity id",
	ErrRelationNameMismatch:  "relation map key does not match relation name",
	ErrInvalidRelationKind:   "relation must set exactly one of allowed types or expression",
	ErrDuplicateType:         "duplicate allowed type",
	ErrInvalidExpression:     "invalid relation expression",
	ErrUnknownNamespace:      "unknown namespace",
	ErrUnknownRelation:       "unknown relation",
	ErrInvalidTupleset:       "arrow tupleset must be a direct relation",
	ErrCyclicRelation:        "cyclic relation reference",
	ErrNamespaceMismatch:     "namespace mismatch",
	ErrRelationNotDirect:     "relation is not direct",
	ErrSubjectTypeNotAllowed: "subject type not allowed",
	ErrMaxDepthExceeded:      "max evaluation depth exceeded",
	ErrNamespaceInUse:        "namespace is referenced by another namespace",
	ErrInvalidLimit:          "limit must not be negative",
}

func (e Error) Error() string {
	if s, ok := errorText[e]; ok {
		return s
	}
	return "unknown error"
}
