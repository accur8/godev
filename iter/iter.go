package iter

type IteratorS[A any] struct {
	Next  func() bool
	Value func() A
}

func (iter *IteratorS[A]) ForEach(fn func(A)) {
	for iter.Next() {
		fn(iter.Value())
	}
}

func (iter *IteratorS[A]) ForEachE(fn func(A) error) error {
	for iter.Next() {
		err := fn(iter.Value())
		if err != nil {
			return err
		}
	}
	return nil
}

func Empty[A any]() *IteratorS[A] {
	return &IteratorS[A]{
		Next:  func() bool { return false },
		Value: func() A { panic("Empty iterator") },
	}
}

func (iter *IteratorS[A]) ToArray() []A {
	var arr []A
	iter.ForEach(func(a A) {
		arr = append(arr, a)
	})
	return arr
}

func FromSlice[A any](slice []A) *IteratorS[A] {
	var i int = -1
	return &IteratorS[A]{
		Next: func() bool {
			i++
			return i < len(slice)
		},
		Value: func() A {
			return slice[i]
		},
	}
}

func FromFn[A any](fn func() (A, bool)) *IteratorS[A] {
	var val A
	return &IteratorS[A]{
		Next: func() bool {
			val0, ok := fn()
			val = val0
			return ok
		},
		Value: func() A {
			return val
		},
	}
}

func (iter *IteratorS[A]) ToMap(keyGetterFn func(A) string) map[string]A {
	result := make(map[string]A)
	for iter.Next() {
		result[keyGetterFn(iter.Value())] = iter.Value()
	}
	return result
}

func (iter *IteratorS[A]) Concat(iters ...*IteratorS[A]) *IteratorS[A] {
	var current *IteratorS[A]
	current = iter
	var i int = -1
	return &IteratorS[A]{
		Next: func() bool {
			for !current.Next() {
				i++
				if i >= len(iters) {
					return false
				}
				current = iters[i]
			}
			return true
		},
		Value: current.Value,
	}
}

func Map[A any, B any](source *IteratorS[A], fn func(A) B) *IteratorS[B] {
	return &IteratorS[B]{
		Next:  source.Next,
		Value: func() B { return fn(source.Value()) },
	}
}

func (iter *IteratorS[A]) MapA(fn func(A) A) *IteratorS[A] {
	return &IteratorS[A]{
		Next:  iter.Next,
		Value: func() A { return fn(iter.Value()) },
	}
}

func (iter *IteratorS[A]) Filter(fn func(A) bool) *IteratorS[A] {
	return &IteratorS[A]{
		Next: func() bool {
			for iter.Next() {
				if fn(iter.Value()) {
					return true
				}
			}
			return false
		},
		Value: iter.Value,
	}
}
