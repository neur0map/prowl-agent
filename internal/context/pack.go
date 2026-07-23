package context

import "fmt"

// Pack ranks and fits candidates into an explicit estimated context budget.
func Pack(request Request, candidates []Candidate, estimator CostEstimator) (Packet, error) {
	if err := request.Validate(); err != nil {
		return Packet{}, err
	}
	if estimator == nil {
		estimator = ByteQuarterEstimator{}
	}
	if request.BudgetTokens == 0 && request.BudgetBytes == 0 {
		request.BudgetTokens = 1800
	}
	packet := emptyPacket(request)
	packet.Budget.RequestedTokens = request.BudgetTokens
	packet.Budget.RequestedBytes = request.BudgetBytes
	ranked := diversify(RankCandidates(candidates))
	seen := map[string]bool{}
	for _, candidate := range ranked {
		if seen[candidate.ID] {
			packet.Omitted["duplicate"]++
			continue
		}
		seen[candidate.ID] = true
		content, usedMode := contentForMode(candidate, request.Mode)
		costText := candidate.Title + "\n" + candidate.Summary + "\n" + content
		tokens := estimator.Tokens(costText)
		bytesCost := len([]byte(costText))
		if !fits(packet.Budget, request, tokens, bytesCost) && request.Mode != ModeCompact {
			content, usedMode = contentForMode(candidate, ModeCompact)
			costText = candidate.Title + "\n" + candidate.Summary + "\n" + content
			tokens = estimator.Tokens(costText)
			bytesCost = len([]byte(costText))
		}
		if !fits(packet.Budget, request, tokens, bytesCost) {
			packet.Omitted["budget"]++
			continue
		}
		item := candidate.Item
		item.Content = content
		item.EstimatedTokens = tokens
		item.WhySelected = uniqueStrings(append(item.WhySelected, "packed as "+string(usedMode)))
		if item.WhySelected == nil {
			item.WhySelected = []string{}
		}
		if item.Audience == nil {
			item.Audience = []string{"assistant", "user"}
		}
		if item.Citations == nil {
			item.Citations = []Citation{}
		}
		packet.Items = append(packet.Items, item)
		packet.Budget.EstimatedTokens += tokens
		packet.Budget.EstimatedBytes += bytesCost
	}
	packet.Summary = fmt.Sprintf("Selected %d context item(s) within the requested estimated budget.", len(packet.Items))
	if packet.Omitted["budget"] > 0 {
		packet.Next = append(packet.Next, "Increase the budget or fetch a selected detail_resource directly.")
	}
	return packet, nil
}

func contentForMode(candidate Candidate, mode Mode) (string, Mode) {
	switch mode {
	case ModeFull:
		if candidate.FullContent != "" {
			return candidate.FullContent, ModeFull
		}
		fallthrough
	case ModeStandard:
		if candidate.StandardContent != "" {
			return candidate.StandardContent, ModeStandard
		}
		fallthrough
	default:
		return candidate.CompactContent, ModeCompact
	}
}

func fits(current Budget, request Request, tokens, bytesCost int) bool {
	if request.BudgetTokens > 0 && current.EstimatedTokens+tokens > request.BudgetTokens {
		return false
	}
	if request.BudgetBytes > 0 && current.EstimatedBytes+bytesCost > request.BudgetBytes {
		return false
	}
	return true
}

func diversify(candidates []Candidate) []Candidate {
	var first, repeats []Candidate
	seenSource := map[string]bool{}
	for _, candidate := range candidates {
		source := candidate.ID
		if len(candidate.Citations) > 0 && candidate.Citations[0].Path != "" {
			source = candidate.Citations[0].Path
		}
		if !seenSource[source] {
			seenSource[source] = true
			first = append(first, candidate)
		} else {
			repeats = append(repeats, candidate)
		}
	}
	return append(first, repeats...)
}
