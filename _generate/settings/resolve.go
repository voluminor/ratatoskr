package main

import (
	"slices"
	"strings"

	dep "github.com/voluminor/ratatoskr/_generate"
)

// // // // // // // // // //

// ResolvedObj holds the results of the flag resolution phase.
type ResolvedObj struct {
	Flags           []FlagObj
	Enums           []EnumObj
	Tree            map[string]*TreeLeafObj
	TypesImports    map[string]bool
	FlagsImports    map[string]bool
	DefaultsImports map[string]bool
	HasCustomFlags  bool
	HasTriggerFlags bool
}

// //

// ResolveFlags takes raw flags from the YAML walk and produces fully resolved
// enums, typed flags, import sets, and the struct tree.
func ResolveFlags(flags []FlagObj, branchUsage map[string]string) ResolvedObj {
	r := ResolvedObj{
		Flags:           flags,
		Tree:            make(map[string]*TreeLeafObj),
		TypesImports:    map[string]bool{},
		FlagsImports:    map[string]bool{"flag": true},
		DefaultsImports: map[string]bool{},
	}

	enumMap := buildEnums(r.Flags, &r.Enums)

	for i := range r.Flags {
		f := &r.Flags[i]
		resolveFlag(f, enumMap, &r)
		insertFlagLeaf(f, r.Tree, branchUsage, enumMap)
	}

	// Defaults imports: check for time.Duration in default literals
	for _, f := range r.Flags {
		if !f.IsTrigger && strings.Contains(f.GoDefault, "time.Duration") {
			r.DefaultsImports["time"] = true
		}
	}

	return r
}

// //

// buildEnums deduplicates enum definitions across flags that share identical value sets.
func buildEnums(flags []FlagObj, enums *[]EnumObj) map[string]*EnumObj {
	enumMap := make(map[string]*EnumObj)
	byValues := make(map[string]*EnumObj)

	for i := range flags {
		f := &flags[i]
		if !f.IsEnum {
			continue
		}

		valKey := strings.Join(f.Enum, "\x00")
		if existing, ok := byValues[valKey]; ok {
			enumMap[f.Name] = existing
			continue
		}

		typeName := buildEnumTypeName(f.Name)
		consts := make([]string, 0, len(f.Enum))
		for _, v := range f.Enum {
			consts = append(consts, buildEnumConstName(typeName, v))
		}

		*enums = append(*enums, EnumObj{
			TypeName:  typeName,
			Values:    slices.Clone(f.Enum),
			GoConsts:  consts,
			ParseFunc: "Parse" + typeName,
			NamesVar:  lowerFirst(typeName) + "Names",
		})

		ref := &(*enums)[len(*enums)-1]
		byValues[valKey] = ref
		enumMap[f.Name] = ref
	}

	return enumMap
}

// //

// resolveFlag populates Go-level fields on a single flag and updates import sets.
func resolveFlag(f *FlagObj, enumMap map[string]*EnumObj, r *ResolvedObj) {
	enumTypeName := ""
	if f.IsEnum {
		enumTypeName = enumMap[f.Name].TypeName
	}

	f.GoType = goTypeResolved(f.Type, f.IsEnum, enumTypeName, f.IsArray)
	f.GoDefault = goDefaultLiteral(*f, enumTypeName)
	f.FlagAccessor = buildFlagAccessor(f.Name)
	f.EnumType = enumTypeName

	if parts := strings.SplitN(f.Name, ".", 2); len(parts) > 1 {
		f.Group = parts[0]
	}

	bt := f.Type
	if f.IsArray {
		bt = baseType(bt)
	}

	if bt == "duration" {
		r.TypesImports["time"] = true
		if f.IsArray {
			r.FlagsImports["time"] = true
		}
	}
	if isCustomFlag(bt, f.IsEnum, f.IsArray) {
		r.HasCustomFlags = true
	}
	if f.IsMap {
		r.HasCustomFlags = true
		r.FlagsImports["encoding/json"] = true
	}
	if f.IsArray {
		r.FlagsImports["strings"] = true
	}
	if f.IsTrigger {
		r.HasTriggerFlags = true
	}
	if !f.IsEnum && !f.IsArray && !f.IsMap && !nativeFlagTypes[f.Type] {
		r.FlagsImports["fmt"] = true
		r.FlagsImports["strconv"] = true
	}
}

// //

// insertFlagLeaf creates a tree leaf for the flag and inserts it into the struct tree.
func insertFlagLeaf(f *FlagObj, tree map[string]*TreeLeafObj, branchUsage map[string]string, enumMap map[string]*EnumObj) {
	points := strings.Split(f.Name, ".")
	enumTypeName := ""
	if f.IsEnum {
		enumTypeName = enumMap[f.Name].TypeName
	}

	insertLeaf(tree, points, &TreeLeafObj{
		Name:      dep.GenGoName(points[len(points)-1]),
		Type:      f.GoType,
		Key:       points[len(points)-1],
		IsEnum:    f.IsEnum,
		IsArray:   f.IsArray,
		IsMap:     f.IsMap,
		IsTrigger: f.IsTrigger,
		EnumType:  enumTypeName,
	}, branchUsage)
}
