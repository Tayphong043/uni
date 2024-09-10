//go:build generate

package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"zgo.at/termtext"
	"zgo.at/zli"
	"zgo.at/zstd/zslice"
	"zgo.at/zstd/zstring"
)

const (
	GenderNone = 0
	GenderSign = 1
	GenderRole = 2
)

type (
	EmojiGroup    uint8
	EmojiSubgroup uint16
	Emoji         struct {
		Codepoints []rune
		Name       string
		Group      EmojiGroup
		Subgroup   EmojiSubgroup
		CLDR       []string
		SkinTones  bool
		Genders    int
	}
)

func readCLDR(f string) map[string][]string {
	d, err := os.ReadFile(f)
	zli.F(err)

	var cldr struct {
		Annotations []struct {
			CP    string `xml:"cp,attr"`
			Type  string `xml:"type,attr"`
			Names string `xml:",innerxml"`
		} `xml:"annotations>annotation"`
	}
	zli.F(xml.Unmarshal(d, &cldr))

	var (
		// "Good enough" XML entity removal.
		tr  = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&")
		out = make(map[string][]string)
	)
	for _, a := range cldr.Annotations {
		if a.Type != "tts" {
			a.CP = strings.ReplaceAll(a.CP, "\u200d", "")
			out[a.CP] = strings.Split(tr.Replace(a.Names), " | ")
		}
	}
	return out
}

func main() {
	if len(os.Args) != 3 {
		zli.Fatalf("usage: emojis.go [emoji-test.txt] [cldr-en.xml]")
	}

	cldr := readCLDR(os.Args[2])
	text, err := os.ReadFile(os.Args[1])
	zli.F(err)

	var (
		emo             = make([]Emoji, 0, 2048)
		signGender      = make(map[rune]struct{})
		group, subgroup string
		groups          []string
		subgroups       = make(map[string][]string)
		groupID         EmojiGroup
		subgroupID      EmojiSubgroup
		lines           = strings.Split(string(text), "\n")
	)
	lines = slices.DeleteFunc(lines, func(l string) bool {
		return !(strings.Contains(l, "group:") || strings.Contains(l, "subgroup:") || strings.Contains(l, "; fully-qualified"))
	})
	lines = append(lines, "") /// So we don't need to check len for lines[i+1] below.

	for i, line := range lines {
		/// Groups are listed as a comment, but we want to preserve them.
		///   # group: Smileys & Emotion
		///   # subgroup: face-smiling
		if strings.HasPrefix(line, "# group: ") {
			group = line[strings.Index(line, ":")+2:]
			groups = append(groups, group)
			groupID++
			continue
		}
		if strings.HasPrefix(line, "# subgroup: ") {
			subgroup = line[strings.Index(line, ":")+2:]
			subgroups[group] = append(subgroups[group], subgroup)
			subgroupID++
			continue
		}

		var comment string
		if p := strings.Index(line, "#"); p > -1 {
			comment, line = strings.TrimSpace(line[p+1:]), strings.TrimSpace(line[:p])
		}
		if len(line) == 0 {
			continue
		}

		/// "only fully-qualified emoji zwj sequences should be generated by
		/// keyboards and other user input devices"
		if !strings.HasSuffix(line, "; fully-qualified") {
			continue
		}

		codepoints := func() []rune {
			s := strings.Split(strings.TrimSpace(strings.Split(line, ";")[0]), " ")
			all := make([]rune, 0, len(s))
			for _, c := range s {
				r, err := strconv.ParseInt(string(c), 16, 32)
				zli.F(err)
				if r != 0x200d { /// Skip ZWJ; we construct it ourself.
					all = append(all, rune(r))
				}
			}
			return all
		}()

		/// Skin tones; we just want the base emojis here; we detect skin tones later.
		if zslice.ContainsAny(codepoints, 0x1f3fb, 0x1f3fc, 0x1f3fd, 0x1f3fe, 0x1f3ff) {
			continue
		}

		/// Male/female sign; store that we saw this.
		if len(codepoints) > 1 && zslice.ContainsAny(codepoints, 0x2640, 0x2642) {
			signGender[codepoints[0]] = struct{}{}
			continue
		}

		if zslice.ContainsAny(codepoints,
			0x1f468, 0x1f469, /// Man/woman emoji
			/// Exceptions, because reasons.
			//0x1f474, 0x1f475, /// 0x1f9d3 🧓 older person / 0x1f474 👴 old man / 0x1f475 👵 old woman
			//0x1f930, 0x1fac3, /// 0x1fac4 🫄  pregnant person / 0x1f930 🤰 pregnant woman / 0x1fac3 🫃  pregnant man

			// TODO:
			// [1f9d1 200d 1f384] 🧑🎄 mx claus / [1f385] 🎅 Santa Claus / [1f936] 🤶 Mrs. Claus

			// TODO: gender, how?
			// 1F483    ; fully-qualified     # 💃 E0.6 woman dancing
			// 1F57A    ; fully-qualified     # 🕺 E3.0 man dancing
		) {
			continue
		}

		tone := strings.ContainsAny(lines[i+1], "\U0001f3fb\U0001f3fc\U0001f3fd\U0001f3fe\U0001f3ff")
		gender := GenderNone

		// Old/classic gendered emoji. A "person" emoji is combined with "female
		// sign" or "male sign" to make an explicitly gendered one:
		//
		//   1F937                 # 🤷 E4.0 person shrugging
		//   1F937 200D 2642 FE0F  # 🤷‍♂️ E4.0 man shrugging
		//   1F937 200D 2640 FE0F  # 🤷‍♀️ E4.0 woman shrugging
		//
		//   2640                  # ♀ E4.0 female sign
		//   2642                  # ♂ E4.0 male sign
		//
		// Detect: 2640 or 2642 occurs in sequence position>0 to exclude just
		// the female/male signs.
		// Just keep a static list for now; this is essentially unchanging.
		// GenderSign

		// Newer gendered emoji; combine "person", "man", or "women" with
		// something related to that:
		//
		//   1F9D1 200D 2695 FE0F # 🧑‍⚕️ E12.1 health worker
		//   1F468 200D 2695 FE0F # 👨‍⚕️ E4.0 man health worker
		//   1F469 200D 2695 FE0F # 👩‍⚕️ E4.0 woman health worker
		//
		//   1F9D1                # 🧑 E5.0 person
		//   1F468                # 👨 E2.0 man
		//   1F469                # 👩 E2.0 woman
		//
		// Detect: These only appear in the person-role and person-activity
		// subgroups; the special cases only in family subgroup.
		if codepoints[0] == 0x1f9d1 {
			gender = GenderRole
		}

		emo = append(emo, Emoji{
			Codepoints: codepoints,
			Name:       strings.SplitN(comment, " ", 3)[2],
			Group:      groupID - 1,
			Subgroup:   subgroupID - 1,
			SkinTones:  tone,
			Genders:    gender,
			CLDR:       cldr[strings.ReplaceAll(strings.ReplaceAll(string(codepoints), "\ufe0f", ""), "\ufe0e", "")],
		})
	}

	// Add genders indicated by male/female sign.
	for i, e := range emo {
		if len(e.Codepoints) == 1 || (len(e.Codepoints) == 2 && e.Codepoints[1] == 0xfe0f) {
			_, ok := signGender[e.Codepoints[0]]
			if ok {
				emo[i].Genders = GenderSign
			}
		}
	}
	//for g := range signGender {
	//	fmt.Printf("%X %s\n", g, string(g))
	//}
	//return

	fmt.Print("// Code generated by gen.zsh; DO NOT EDIT\n\npackage unidata\n\n")

	{ // Write groups.
		fmt.Println("// Emoji groups.\nconst (")
		fmt.Printf("\t%s = EmojiGroup(iota)\n", mkconst(groups[0]))
		for _, g := range groups[1:] {
			fmt.Printf("\t%s\n", mkconst(g))
		}
		fmt.Print(")\n\n")

		fmt.Println("// EmojiGroups is a list of all emoji groups.")
		fmt.Println("var EmojiGroups = map[EmojiGroup]struct{")
		fmt.Println("\tName      string")
		fmt.Println("\tSubgroups []EmojiSubgroup")
		fmt.Println("}{")
		for _, g := range groups {
			var sg []string
			for _, s := range subgroups[g] {
				sg = append(sg, mkconst(s))
			}
			fmt.Printf("\t%s: {%q, []EmojiSubgroup{\n\t\t%s}},\n", mkconst(g), g,
				termtext.WordWrap(strings.Join(sg, ", "), 100, "\t\t"))
		}
		fmt.Print("}\n\n")
	}
	{ // Write subgroups.
		fmt.Println("// Emoji subgroups.\nconst (")
		first := true
		for _, g := range groups {
			for _, sg := range subgroups[g] {
				if first {
					fmt.Printf("\t%s = EmojiSubgroup(iota)\n", mkconst(sg))
					first = false
				} else {
					fmt.Printf("\t%s\n", mkconst(sg))
				}
			}
		}
		fmt.Print(")\n\n")

		fmt.Println("// EmojiSubgroups is a list of all emoji subgroups.")
		fmt.Println("var EmojiSubgroups = map[EmojiSubgroup]struct{")
		fmt.Println("\tGroup EmojiGroup")
		fmt.Println("\tName  string")
		fmt.Println("}{")
		for _, g := range groups {
			for _, sg := range subgroups[g] {
				fmt.Printf("\t%s: {%s, %q},\n", mkconst(sg), mkconst(g), sg)
			}
		}
		fmt.Print("}\n\n")
	}
	{ // Write emojis
		fmt.Println("var Emojis = []Emoji{")
		for _, e := range emo {
			var cp string
			for _, c := range e.Codepoints {
				cp += fmt.Sprintf("0x%x, ", c)
			}
			cp = cp[:len(cp)-2]

			///                   CP   Name Grp Sgr CLDR sk  gnd
			fmt.Printf("\t{[]rune{%s}, %q,  %d, %d, %#v, %t, %d},\n",
				cp, e.Name, e.Group, e.Subgroup, e.CLDR, e.SkinTones, e.Genders)
		}
		fmt.Print("}\n\n")
	}
}

func mkconst(n string) string {
	dash := zstring.IndexAll(n, "-")
	for i := len(dash) - 1; i >= 0; i-- {
		d := dash[i]
		n = n[:d] + string(n[d+1]^0x20) + n[d+2:]
	}
	return "Emoji" + zstring.UpperFirst(strings.ReplaceAll(n, " & ", "And"))
}
