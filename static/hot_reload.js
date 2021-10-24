var petra = {
  reconnected: () => {
    // location.reload();
    console.log("reconnected");
    if (window.shouldReloadOnReconnect) {
      console.log("refreshing page");
      shouldReloadOnReconnect = false;
      location.reload();
    }
  },
  disconnected: () => {
    // location.reload();
    console.log("disconnected");
    window.shouldReloadOnReconnect = true;
  },
  mounted: function () {
    console.log("mounted");
  },
};

console.log("setting hoooks");
if (window.Hooks) {
  window.Hooks.petra = petra;
} else {
  window.Hooks = {
    petra: petra,
  };
}
