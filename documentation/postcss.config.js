module.exports = {
  plugins: [
    require(require.resolve("@tailwindcss/postcss", { paths: [__dirname] })),
  ],
};
