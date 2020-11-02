// Copyright 2017-present The Hugo Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package output

import (
	"strings"
	"sync"

	"github.com/gothamhq/gotham/helpers"
)

// These may be used as content sections with potential conflicts. Avoid that.
var reservedSections = map[string]bool{
	"shortcodes": true,
	"partials":   true,
}

// LayoutDescriptor describes how a layout should be chosen. This is
// typically built from a Page.
type LayoutDescriptor struct {
	Type    string
	Section string
	Kind    string
	Lang    string
	Layout  string
	// LayoutOverride indicates what we should only look for the above layout.
	LayoutOverride bool

	RenderingHook bool
	Baseof        bool
}

func (d LayoutDescriptor) isList() bool {
	return !d.RenderingHook && d.Kind != "page" && d.Kind != "404"
}

// LayoutHandler calculates the layout template to use to render a given output type.
type LayoutHandler struct {
	mu    sync.RWMutex
	cache map[layoutCacheKey][]string
}

type layoutCacheKey struct {
	d LayoutDescriptor
	f string
}

// NewLayoutHandler creates a new LayoutHandler.
func NewLayoutHandler() *LayoutHandler {
	return &LayoutHandler{cache: make(map[layoutCacheKey][]string)}
}

// For returns a layout for the given LayoutDescriptor and options.
// Layouts are rendered and cached internally.
func (l *LayoutHandler) For(d LayoutDescriptor, f Format) ([]string, error) {
	// We will get lots of requests for the same layouts, so avoid recalculations.
	key := layoutCacheKey{d, f.Name}
	l.mu.RLock()
	if cacheVal, found := l.cache[key]; found {
		l.mu.RUnlock()
		return cacheVal, nil
	}
	l.mu.RUnlock()

	layouts := resolvePageTemplate(d, f)

	layouts = helpers.UniqueStringsReuse(layouts)

	l.mu.Lock()
	l.cache[key] = layouts
	l.mu.Unlock()

	return layouts, nil
}

type layoutBuilder struct {
	layoutVariations []string
	typeVariations   []string
	d                LayoutDescriptor
	f                Format
}

func (l *layoutBuilder) addLayoutVariations(vars ...string) {
	for _, layoutVar := range vars {
		if l.d.Baseof && layoutVar != "baseof" {
			l.layoutVariations = append(l.layoutVariations, layoutVar+"-baseof")
			continue
		}
		if !l.d.RenderingHook && !l.d.Baseof && l.d.LayoutOverride && layoutVar != l.d.Layout {
			continue
		}
		l.layoutVariations = append(l.layoutVariations, layoutVar)
	}
}

func (l *layoutBuilder) addTypeVariations(vars ...string) {
	for _, typeVar := range vars {
		if !reservedSections[typeVar] {
			if l.d.RenderingHook {
				typeVar = typeVar + renderingHookRoot
			}
			l.typeVariations = append(l.typeVariations, typeVar)
		}
	}
}

func (l *layoutBuilder) addSectionType() {
	if l.d.Section != "" {
		l.addTypeVariations(l.d.Section)
	}
}

func (l *layoutBuilder) addKind() {
	l.addLayoutVariations(l.d.Kind)
	l.addTypeVariations(l.d.Kind)
}

const renderingHookRoot = "/_markup"

func resolvePageTemplate(d LayoutDescriptor, f Format) []string {
	b := &layoutBuilder{d: d, f: f}

	if !d.RenderingHook && d.Layout != "" {
		b.addLayoutVariations(d.Layout)
	}
	if d.Type != "" {
		b.addTypeVariations(d.Type)
	}

	if d.RenderingHook {
		b.addLayoutVariations(d.Kind)
		b.addSectionType()
	}

	switch d.Kind {
	case "page":
		b.addLayoutVariations("single")
		b.addSectionType()
	case "home":
		b.addLayoutVariations("index", "home")
		// Also look in the root
		b.addTypeVariations("")
	case "section":
		if d.Section != "" {
			b.addLayoutVariations(d.Section)
		}
		b.addSectionType()
		b.addKind()
	case "term":
		b.addKind()
		if d.Section != "" {
			b.addLayoutVariations(d.Section)
		}
		b.addLayoutVariations("taxonomy")
		b.addTypeVariations("taxonomy")
		b.addSectionType()
	case "taxonomy":
		if d.Section != "" {
			b.addLayoutVariations(d.Section + ".terms")
		}
		b.addSectionType()
		b.addLayoutVariations("terms")
		// For legacy reasons this is deliberately put last.
		b.addKind()
	case "404":
		b.addLayoutVariations("404")
		b.addTypeVariations("")
	}

	isRSS := f.Name == RSSFormat.Name
	if !d.RenderingHook && !d.Baseof && isRSS {
		// The historic and common rss.xml case
		b.addLayoutVariations("")
	}

	if d.Baseof || d.Kind != "404" {
		// Most have _default in their lookup path
		b.addTypeVariations("_default")
	}

	if d.isList() {
		// Add the common list type
		b.addLayoutVariations("list")
	}

	if d.Baseof {
		b.addLayoutVariations("baseof")
	}

	layouts := b.resolveVariations()

	if !d.RenderingHook && !d.Baseof && isRSS {
		layouts = append(layouts, "_internal/_default/rss.xml")
	}

	return layouts
}

func (l *layoutBuilder) resolveVariations() []string {
	var layouts []string

	var variations []string
	name := strings.ToLower(l.f.Name)

	if l.d.Lang != "" {
		// We prefer the most specific type before language.
		variations = append(variations, []string{l.d.Lang + "." + name, name, l.d.Lang}...)
	} else {
		variations = append(variations, name)
	}

	variations = append(variations, "")

	for _, typeVar := range l.typeVariations {
		for _, variation := range variations {
			for _, layoutVar := range l.layoutVariations {
				if variation == "" && layoutVar == "" {
					continue
				}

				s := constructLayoutPath(typeVar, layoutVar, variation, l.f.MediaType.Suffix())
				if s != "" {
					layouts = append(layouts, s)
				}
			}
		}
	}

	return layouts
}

// constructLayoutPath constructs a layout path given a type, layout,
// variations, and extension.  The path constructed follows the pattern of
// type/layout.variations.extension.  If any value is empty, it will be left out
// of the path construction.
//
// Path construction requires at least 2 of 3 out of layout, variations, and extension.
// If more than one of those is empty, an empty string is returned.
func constructLayoutPath(typ, layout, variations, extension string) string {
	// we already know that layout and variations are not both empty because of
	// checks in resolveVariants().
	if extension == "" && (layout == "" || variations == "") {
		return ""
	}

	// Commence valid path construction...

	var (
		p       strings.Builder
		needDot bool
	)

	if typ != "" {
		p.WriteString(typ)
		p.WriteString("/")
	}

	if layout != "" {
		p.WriteString(layout)
		needDot = true
	}

	if variations != "" {
		if needDot {
			p.WriteString(".")
		}
		p.WriteString(variations)
		needDot = true
	}

	if extension != "" {
		if needDot {
			p.WriteString(".")
		}
		p.WriteString(extension)
	}

	return p.String()
}
