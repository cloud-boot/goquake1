// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package progs

// FindField returns the FieldDef whose name matches s, or nil when
// no field has that name. Names are matched against the string-table
// offset via (*Progs).String. tyrquake: ED_FindField.
func (p *Progs) FindField(name string) *Def {
	for i := range p.FieldDefs {
		if p.String(p.FieldDefs[i].SName) == name {
			return &p.FieldDefs[i]
		}
	}
	return nil
}

// FindGlobal returns the GlobalDef whose name matches s, or nil
// when no global has that name. tyrquake: ED_FindGlobal.
func (p *Progs) FindGlobal(name string) *Def {
	for i := range p.GlobalDefs {
		if p.String(p.GlobalDefs[i].SName) == name {
			return &p.GlobalDefs[i]
		}
	}
	return nil
}

// FindFunction returns the Function whose name matches s and its
// 1-based index, or (nil, -1) when no function has that name.
// tyrquake: ED_FindFunction.
func (p *Progs) FindFunction(name string) (*Function, int) {
	for i := range p.Functions {
		if p.String(p.Functions[i].SName) == name {
			return &p.Functions[i], i
		}
	}
	return nil, -1
}

// FieldAtOfs returns the FieldDef whose Ofs equals ofs, or nil when
// no field sits at that offset. tyrquake: ED_FieldAtOfs (used by
// debug pretty-printers).
func (p *Progs) FieldAtOfs(ofs uint16) *Def {
	for i := range p.FieldDefs {
		if p.FieldDefs[i].Ofs == ofs {
			return &p.FieldDefs[i]
		}
	}
	return nil
}

// GlobalAtOfs is the global-pool analogue of FieldAtOfs.
// tyrquake: ED_GlobalAtOfs.
func (p *Progs) GlobalAtOfs(ofs uint16) *Def {
	for i := range p.GlobalDefs {
		if p.GlobalDefs[i].Ofs == ofs {
			return &p.GlobalDefs[i]
		}
	}
	return nil
}
