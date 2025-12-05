package types

// ImplRegistry stores explicit implementations of behaviors for structs.
type ImplRegistry struct {
	impls map[string]map[string]bool // structName -> behaviorName -> true
}

func NewImplRegistry() *ImplRegistry {
	return &ImplRegistry{
		impls: make(map[string]map[string]bool),
	}
}

// Register records that a struct implements a behavior.
func (r *ImplRegistry) Register(structName string, behaviorName string) {
	if _, ok := r.impls[structName]; !ok {
		r.impls[structName] = make(map[string]bool)
	}
	r.impls[structName][behaviorName] = true
}

// Implements checks if a struct implements a behavior.
func (r *ImplRegistry) Implements(structName string, behaviorName string) bool {
	if behaviors, ok := r.impls[structName]; ok {
		return behaviors[behaviorName]
	}
	return false
}

// IsAssignable checks if source type can be assigned to target type.
// It handles Unions, Intersections, and explicit Behavior implementations.
func IsAssignable(target, source Type, registry *ImplRegistry) bool {
	if target.Equals(source) {
		return true
	}

	if target == Any {
		return true
	}

	// 1. If Source is a Union, ALL its members must be assignable to Target.
	//    e.g. (A | B) -> A is FALSE (because B might not be A)
	//    e.g. (A | B) -> (A | B | C) is TRUE
	if sourceUnion, ok := source.(*UnionType); ok {
		for _, member := range sourceUnion.Types {
			if !IsAssignable(target, member, registry) {
				return false
			}
		}
		return true
	}

	// 2. If Target is a Union, Source must be assignable to AT LEAST ONE member.
	//    e.g. A -> (A | B) is TRUE
	if targetUnion, ok := target.(*UnionType); ok {
		for _, member := range targetUnion.Types {
			if IsAssignable(member, source, registry) {
				return true
			}
		}
		return false
	}

	// 3. If Target is an Intersection, Source must be assignable to ALL members.
	//    e.g. Struct -> (Behavior1 & Behavior2) requires Struct to impl both.
	if targetIntersection, ok := target.(*IntersectionType); ok {
		for _, behavior := range targetIntersection.Behaviors {
			if !IsAssignable(behavior, source, registry) {
				return false
			}
		}
		return true
	}

	// 4. If Target is a Behavior
	if targetBehavior, ok := target.(*BehaviorType); ok {
		// Case 4a: Source is a Struct -> Check Registry
		if structType, ok := source.(*StructType); ok {
			return registry.Implements(structType.Name, targetBehavior.Name)
		}
		
		// Case 4b: Source is an Intersection -> Check if it contains the behavior
		if sourceIntersection, ok := source.(*IntersectionType); ok {
			for _, b := range sourceIntersection.Behaviors {
				if b.Equals(targetBehavior) {
					return true
				}
			}
		}
	}

	return false
}
