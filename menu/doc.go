// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package menu implements Quake's classic main menu state machine
// (title screen / new game / skill select / options / load / save /
// quit). The menu is a self-contained subsystem driven by the runloop:
// while [State] is non-[StateNone] the runloop suppresses the 3D
// world pass and calls [Menu.Draw] in the Pre2DDraw slot; once the
// player picks "play" the menu transitions to [StateNone] and the
// runloop unfreezes the game pipeline.
//
// tyrquake: menu.c -- the M_Init / M_Draw / M_Keydown trio, with
// m_state and the per-screen MENU_* sub-states.
//
// PORT NOTES:
//
//   - The Go port collapses tyrquake's free-standing m_main / m_load /
//     m_save / m_options globals into a single [Menu] struct that owns
//     the cursor index per screen.
//
//   - The asset set (qplaque / ttl_main / mainmenu / menudot1..6 /
//     p_option / p_save / ttl_sgl / ttl_load / ttl_cstm) is supplied
//     by the caller as an [Assets] bundle. Missing assets fall back
//     to a text label so the menu stays navigable on bring-up builds
//     where the gfx.wad has not been loaded yet.
//
//   - [Menu.Handle] is the per-frame key dispatch; it returns true
//     when the user has picked "start a new game" so the runloop can
//     flip its world-pass gate without reaching into menu internals.
//
//   - The skill ladder is the upstream 0..3 enum (Easy / Normal /
//     Hard / Nightmare). Selected via the skill sub-menu and exposed
//     on [Menu.SkillLevel] for the runloop / host to forward to the
//     server's `skill` cvar.
package menu
