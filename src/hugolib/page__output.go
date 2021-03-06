// Copyright 2019 The Hugo Authors. All rights reserved.
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

package hugolib

import (
	"github.com/strawberryssg/strawberry-v0/markup/converter"
	"github.com/strawberryssg/strawberry-v0/output"
	"github.com/strawberryssg/strawberry-v0/resources/page"
	"github.com/strawberryssg/strawberry-v0/resources/resource"
)

func newPageOutput(
	ps *pageState,
	pp pagePaths,
	f output.Format,
	render bool) *pageOutput {
	var targetPathsProvider targetPathsHolder
	var linksProvider resource.ResourceLinksProvider

	ft, found := pp.targetPaths[f.Name]
	if !found {
		// Link to the main output format
		ft = pp.targetPaths[pp.firstOutputFormat.Format.Name]
	}
	targetPathsProvider = ft
	linksProvider = ft

	var paginatorProvider page.PaginatorProvider = page.NopPage
	var pag *pagePaginator

	if render && ps.IsNode() {
		pag = newPagePaginator(ps)
		paginatorProvider = pag
	}

	providers := struct {
		page.PaginatorProvider
		resource.ResourceLinksProvider
		targetPather
	}{
		paginatorProvider,
		linksProvider,
		targetPathsProvider,
	}

	po := &pageOutput{
		f:                       f,
		pagePerOutputProviders:  providers,
		ContentProvider:         page.NopPage,
		TableOfContentsProvider: page.NopPage,
		render:                  render,
		paginator:               pag,
	}

	return po
}

// We create a pageOutput for every output format combination, even if this
// particular page isn't configured to be rendered to that format.
type pageOutput struct {
	// Set if this page isn't configured to be rendered to this format.
	render bool

	f output.Format

	// Only set if render is set.
	// Note that this will be lazily initialized, so only used if actually
	// used in template(s).
	paginator *pagePaginator

	// These interface provides the functionality that is specific for this
	// output format.
	pagePerOutputProviders
	page.ContentProvider
	page.TableOfContentsProvider

	// May be nil.
	cp *pageContentOutput
}

func (o *pageOutput) initRenderHooks() error {
	if o.cp == nil {
		return nil
	}

	var initErr error

	o.cp.renderHooks.init.Do(func() {
		ps := o.cp.p

		c := ps.getContentConverter()
		if c == nil || !c.Supports(converter.FeatureRenderHooks) {
			return
		}

		h, err := ps.createRenderHooks(o.f)
		if err != nil {
			initErr = err
			return
		}
		o.cp.renderHooks.hooks = h

		if !o.cp.renderHooksHaveVariants || h.IsZero() {
			// Check if there is a different render hooks template
			// for any of the other page output formats.
			// If not, we can reuse this.
			for _, po := range ps.pageOutputs {
				if po.f.Name != o.f.Name {
					h2, err := ps.createRenderHooks(po.f)
					if err != nil {
						initErr = err
						return
					}

					if h2.IsZero() {
						continue
					}

					if o.cp.renderHooks.hooks.IsZero() {
						o.cp.renderHooks.hooks = h2
					}

					o.cp.renderHooksHaveVariants = !h2.Eq(o.cp.renderHooks.hooks)

					if o.cp.renderHooksHaveVariants {
						break
					}

				}
			}
		}
	})

	return initErr
}

func (p *pageOutput) initContentProvider(cp *pageContentOutput) {
	if cp == nil {
		return
	}
	p.ContentProvider = cp
	p.TableOfContentsProvider = cp
	p.cp = cp
}

func (p *pageOutput) enablePlaceholders() {
	if p.cp != nil {
		p.cp.enablePlaceholders()
	}
}
