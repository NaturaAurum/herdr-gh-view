package main

import "testing"

func nums(prs []PR) []int {
	out := []int{}
	for _, p := range prs {
		out = append(out, p.Number)
	}
	return out
}

func pr(n int, head, base string) PR { return PR{Number: n, Head: head, Base: base} }

func TestStackOf(t *testing.T) {
	open := []PR{
		pr(1, "a", "main"),
		pr(2, "b", "a"),
		pr(3, "c", "b"),
		pr(4, "d", "c"),
		pr(5, "e", "c"), // fork on top of c
		pr(9, "x", "main"),
	}
	cases := []struct {
		cur  PR
		want []int
	}{
		{open[1], []int{1, 2, 3, 4, 5}}, // middle: full chain down, linear up to the fork
		{open[0], []int{1, 2, 3, 4, 5}}, // bottom
		{open[3], []int{1, 2, 3, 4}},    // top of one fork branch
		{open[5], nil},                  // unrelated PR is not a stack
		{pr(7, "y", "gone"), nil},       // base branch without an open PR
	}
	for _, c := range cases {
		got := nums(stackOf(c.cur, open, "main"))
		if c.want == nil && len(got) == 0 {
			continue
		}
		if len(got) != len(c.want) {
			t.Fatalf("#%d: got %v want %v", c.cur.Number, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("#%d: got %v want %v", c.cur.Number, got, c.want)
			}
		}
	}
}
