package iterable

import "accur8.io/godev/iter"

type IterableS[A any] struct {
	Iterator func() *iter.IteratorS[A]
}

func (iterable *IterableS[A]) ForEach(fn func(A)) {
	iterable.Iterator().ForEach(fn)
}

func (iterable *IterableS[A]) NonEmpty() bool {
	return iterable.Iterator().Next()
}

func (iterable *IterableS[A]) IsEmpty() bool {
	return !iterable.Iterator().Next()
}

func Empty[A any]() *IterableS[A] {
	return FromIterFn(
		func() *iter.IteratorS[A] { return iter.Empty[A]() },
	)
}

func (iterable *IterableS[A]) ToArray() []A {
	return iterable.Iterator().ToArray()
}

func FromIterFn[A any](fn func() *iter.IteratorS[A]) *IterableS[A] {
	return &IterableS[A]{
		Iterator: fn,
	}
}

func FromSlice[A any](slice []A) *IterableS[A] {
	return FromIterFn(
		func() *iter.IteratorS[A] { return iter.FromSlice(slice) },
	)
}

func (iterable *IterableS[A]) ToMap(keyGetterFn func(A) string) map[string]A {
	iter := iterable.Iterator()
	result := make(map[string]A)
	for iter.Next() {
		result[keyGetterFn(iter.Value())] = iter.Value()
	}
	return result
}

func (iterable *IterableS[A]) Concat(iterables ...*IterableS[A]) *IterableS[A] {
	return FromIterFn(
		func() *iter.IteratorS[A] {
			i := iterable.Iterator()
			for _, iterable := range iterables {
				i = i.Concat(iterable.Iterator())
			}
			return i
		},
	)
}

func Map[A any, B any](source *IterableS[A], fn func(A) B) *IterableS[B] {
	return FromIterFn(
		func() *iter.IteratorS[B] {
			return iter.Map(
				source.Iterator(),
				fn,
			)
		},
	)
}

func (iterable *IterableS[A]) MapA(fn func(A) A) *IterableS[A] {
	return Map(iterable, fn)
}

func (iterable *IterableS[A]) Filter(fn func(A) bool) *IterableS[A] {
	return FromIterFn(
		func() *iter.IteratorS[A] {
			return iterable.Iterator().Filter(fn)
		},
	)
}

func (iterable *IterableS[A]) Find(fn func(A) bool) (A, bool) {
	var a A
	b := false
	for i := iterable.Iterator(); i.Next(); {
		if fn(i.Value()) {
			a = i.Value()
			b = true
			break
		}
	}
	return a, b
}

func FlattenSlices[T any](seq *IterableS[[]T]) []T {
	var totalLen int

	seq.ForEach(func(t []T) {
		totalLen += len(t)
	})

	result := make([]T, totalLen)

	var i int

	seq.ForEach(func(s []T) {
		i += copy(result[i:], s)
	})

	return result
}
