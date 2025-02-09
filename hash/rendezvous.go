package hash

import "math"

type Rendezvous struct {
	nodes []string
}

func NewRendezvous(nodes []string) *Rendezvous {
	return &Rendezvous{
		nodes: nodes,
	}
}

func (h *Rendezvous) Hash(key []byte) uint64 {
	maxScore := math.MinInt64
	var selected uint64

	// 计算每个节点的得分
	for i, node := range h.nodes {
		score := h.score(key, []byte(node))
		if score > maxScore {
			maxScore = score
			selected = uint64(i)
		}
	}

	return selected
}

func (h *Rendezvous) score(key, node []byte) float64 {
	hash := NewMurmur3(0).Hash(append(key, node...))
	return -math.Log(float64(hash) / float64(math.MaxUint64))
}
