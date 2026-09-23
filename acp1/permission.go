package acp1

import "fmt"

// DefaultPermissionOptions returns the usual choices for a permission
// request: allow once, allow always and reject once.
func DefaultPermissionOptions() []PermissionOption {
	return []PermissionOption{
		NewPermissionOption(PermissionOptionKindAllowOnce, "Allow"),
		NewPermissionOption(PermissionOptionKindAllowAlways, "Always allow"),
		NewPermissionOption(PermissionOptionKindRejectOnce, "Reject"),
	}
}

// NewPermissionOption returns an option of the given kind whose id is the
// kind itself, which is unique as long as a request offers each kind once.
func NewPermissionOption(kind PermissionOptionKind, name string) PermissionOption {
	return PermissionOption{OptionID: PermissionOptionID(kind), Name: name, Kind: kind}
}

// PermissionSelected answers a permission request with the chosen option.
func PermissionSelected(id PermissionOptionID) *RequestPermissionResponse {
	return &RequestPermissionResponse{Outcome: NewRequestPermissionOutcome(RequestPermissionOutcomeSelected{OptionID: id})}
}

// PermissionCancelled answers a permission request whose prompt turn was
// cancelled before the user chose.
func PermissionCancelled() *RequestPermissionResponse {
	return &RequestPermissionResponse{Outcome: NewRequestPermissionOutcome(RequestPermissionOutcomeCancelled{})}
}

// chosen returns the offered option the response selects, and whether it
// allows the action. A cancelled request, or an outcome this SDK does not
// know, selects nothing and allows nothing.
func chosen(options []PermissionOption, response *RequestPermissionResponse) (PermissionOption, bool, error) {
	selected, ok := response.Outcome.As[RequestPermissionOutcomeSelected]()
	if !ok {
		return PermissionOption{}, false, nil
	}
	for _, option := range options {
		if option.OptionID == selected.OptionID {
			allowed := option.Kind == PermissionOptionKindAllowOnce || option.Kind == PermissionOptionKindAllowAlways
			return option, allowed, nil
		}
	}
	return PermissionOption{}, false, fmt.Errorf("acp1: the client selected option %q, which was not offered", selected.OptionID)
}
