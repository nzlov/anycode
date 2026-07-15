package workflow

import "strings"

var mergeOutputFields = []OutputField{
	{Key: "merge.status", Description: "Merge result status.", ValueType: "string"},
	{Key: "merge.failureCode", Description: "Merge failure code when the merge did not complete.", ValueType: "string"},
	{Key: "merge.failureReason", Description: "Merge failure reason when the merge did not complete.", ValueType: "string"},
}

func CanonicalGraph(graph Graph) Graph {
	canonical := GraphWithoutApprovalOutputFields(graph)
	nodes := make([]Node, 0, len(canonical.Nodes))
	for _, node := range canonical.Nodes {
		node.OutputFields = canonicalOutputFields(node)
		nodes = append(nodes, node)
	}
	return Graph{Nodes: nodes, Edges: canonical.Edges}
}

func GraphWithoutApprovalOutputFields(graph Graph) Graph {
	nodes := append([]Node(nil), graph.Nodes...)
	for index, node := range nodes {
		if node.OutputFields == nil {
			continue
		}
		fields := make([]OutputField, 0, len(node.OutputFields))
		for _, field := range node.OutputFields {
			if !IsApprovalOutputField(field.Key) {
				fields = append(fields, field)
			}
		}
		node.OutputFields = fields
		nodes[index] = node
	}
	return Graph{Nodes: nodes, Edges: append([]Edge(nil), graph.Edges...)}
}

func IsApprovalOutputField(key string) bool {
	key = strings.TrimSpace(key)
	return key == "approval" || strings.HasPrefix(key, "approval.")
}

func canonicalOutputFields(node Node) []OutputField {
	fields := make([]OutputField, 0, len(node.OutputFields)+len(mergeOutputFields))
	fields = append(fields, node.OutputFields...)
	if strings.EqualFold(strings.TrimSpace(node.Type), "merge") || node.Merge != nil {
		for _, field := range mergeOutputFields {
			fields = ensureOutputField(fields, field)
		}
	}
	return fields
}

func ensureOutputField(fields []OutputField, required OutputField) []OutputField {
	for index := range fields {
		if strings.TrimSpace(fields[index].Key) == required.Key {
			fields[index] = required
			return fields
		}
	}
	return append(fields, required)
}
