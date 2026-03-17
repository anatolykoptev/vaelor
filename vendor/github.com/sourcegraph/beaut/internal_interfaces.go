package beaut

type semigroupAction[Self any, Action any] interface {
	Combine(other Action) Self
	Join(others ...Action) Self
}

type semigroup[Self any] interface {
	semigroupAction[Self, Self]
}

type simpleContainer interface {
	Len() int
	IsEmpty() bool
}

type byteIndexable interface {
	ByteAt(i ByteIndex) byte
	RuneAt(i ByteIndex) (_ rune, runeLengthInBytes int32)
}

type utf8Sliceable[Self any] interface {
	SliceUTF8(start ByteIndex, end ByteIndex) (Self, error)
	MustSliceUTF8(start ByteIndex, end ByteIndex) Self
}
