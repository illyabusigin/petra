import Alpine from "alpinejs";

window.Alpine = Alpine;

// Alpine handles only browser-local state here. Petra still renders the page,
// and Go still owns routing and static file serving.
Alpine.data("disclosure", () => ({
  open: false,
  toggle() {
    this.open = !this.open;
  },
}));

Alpine.start();
