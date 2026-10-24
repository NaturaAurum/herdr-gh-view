package main

// stackOf returns the stack around cur, bottom (closest to the default branch)
// first. A stack is the chain of open PRs linked by base -> head branches, so
// it works the same for Graphite, spr, or hand-made stacks. Returns nil when
// cur is not stacked on or under any other open PR.
func stackOf(cur PR, open []PR, defaultBranch string) []PR {
	byHead := map[string]PR{}
	for _, p := range open {
		byHead[p.Head] = p
	}

	seen := map[int]bool{cur.Number: true}
	var below []PR
	for base := cur.Base; base != defaultBranch; {
		p, ok := byHead[base]
		if !ok || seen[p.Number] {
			break
		}
		seen[p.Number] = true
		below = append([]PR{p}, below...)
		base = p.Base
	}

	stack := append(below, cur)
	// Walk upward while the chain is linear; at a fork, list the children and stop.
	for head := cur.Head; ; {
		var children []PR
		for _, p := range open {
			if p.Base == head && !seen[p.Number] {
				children = append(children, p)
			}
		}
		for _, c := range children {
			seen[c.Number] = true
		}
		stack = append(stack, children...)
		if len(children) != 1 {
			break
		}
		head = children[0].Head
	}

	if len(stack) == 1 {
		return nil
	}
	return stack
}
