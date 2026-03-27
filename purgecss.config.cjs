module.exports = {
  content: ['static/index.html', 'static/js/**/*.js'],
  css: ['static/css/*.css'],
  safelist: {
    standard: [
      'active',
      'hidden',
      'show',
      'light-theme',
      'highlight-card',
      'is-active',
      'is-loading',
      'is-visible',
      'u-hidden',
      'u-flex',
      /^category-/,
      /^status-/,
      /^priority-/
    ],
    deep: [/^modal/, /^toast/],
    greedy: [/is-selected/]
  },
  output: 'static/css/'
}
