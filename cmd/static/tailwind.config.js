module.exports = {
  mode: "jit",
  purge: {
    enabled: true,
    content: [
      "./*.html",
      "./routes/**/*.{js,jsx,ts,tsx,vue,hbs}",
      "./components/**/*.{js,jsx,ts,tsx,vue,hbs}",
    ],
  },
  theme: [],
  plugins: [require("@tailwindcss/forms")],
};
