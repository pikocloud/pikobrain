package utils

type Set[T comparable] map[T]struct{}

func NewSet[T comparable](values ...T) Set[T] {
	s := make(Set[T], len(values))
	for _, v := range values {
		s[v] = struct{}{}
	}
	return s
}

func (s Set[T]) Add(values ...T) {
	for _, v := range values {
		s[v] = struct{}{}
	}
}

func (s Set[T]) Put(value T) bool {
	if _, ok := s[value]; ok {
		return false
	}
	s[value] = struct{}{}
	return true
}

func (s Set[T]) Remove(values ...T) {
	for _, v := range values {
		delete(s, v)
	}
}

func (s Set[T]) Contains(values ...T) bool {
	for _, v := range values {
		if _, ok := s[v]; ok {
			return true
		}
	}
	return false
}
