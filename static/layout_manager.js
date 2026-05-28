// Iridium Layout Manager — client glue.
//
// Iridium's Alpine build is CSP-mode (no inline expressions), so all reactive
// state and handlers go through a named Alpine.data component registered here.
// The component owns three things:
//
//   1. The local editMode flag (lock / unlock toggle).
//   2. The selected block type in the "Add" dropdown.
//   3. The drag-reorder handler — called by Alpine sort after each move; reads
//      the new DOM order and POSTs it to the server (which re-renders the
//      grid).
//
// Mutations beyond reorder (add / remove / resize / save) are plain htmx
// requests configured directly on the buttons in the templ — no JS needed
// for those.

(function () {
    const init = () => {
        if (!window.Alpine || !window.Alpine.data) {
            // Iridium's Alpine bundle hasn't loaded yet — try again on the
            // next tick. Should be rare; this script is loaded *after* the
            // iridium bundle by the page template.
            setTimeout(init, 50);
            return;
        }

        window.Alpine.data('layout_manager', () => ({
            editMode: false,
            // Index into the data-block-options attribute on the root, mirrors
            // Filament's selectedComponent. The "Add" form posts the value of
            // the <select>, so we just track the index here for the UI.
            selectedType: '',

            init() {
                // Populate the default selected type from the <select> if any
                // options were rendered server-side.
                const select = this.$el.querySelector('[data-lm-type-select]');
                if (select && select.options.length > 0) {
                    this.selectedType = select.options[0].value;
                }
            },

            toggleEdit() {
                this.editMode = !this.editMode;
            },

            // Called by x-sort after the user drops a block. Alpine has
            // already reordered the DOM; we read the new order off [data-id]
            // attributes and POST it.
            handleReorder() {
                const grid = this.$el.querySelector('[data-lm-grid]');
                if (!grid) return;
                const order = Array.from(grid.querySelectorAll('[data-lm-block]'))
                    .map(el => el.dataset.id)
                    .filter(Boolean);
                const url = grid.dataset.reorderUrl;
                if (!url) return;

                fetch(url, {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'HX-Request': 'true',
                    },
                    credentials: 'same-origin',
                    body: JSON.stringify({ order }),
                })
                    .then(r => r.text())
                    .then(html => {
                        // Swap the grid body with the server's re-render so
                        // the source of truth is the server's order, not
                        // whatever Alpine sort left in the DOM.
                        grid.outerHTML = html;
                        // Re-bind: the morph replaced the grid, but Alpine
                        // re-evaluates x-data on the parent so handlers still
                        // wire up automatically.
                    })
                    .catch(err => console.error('[layoutmgr] reorder failed', err));
            },
        }));
    };

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
