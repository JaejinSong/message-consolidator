module.exports = {
  content: ['static/index.html', 'static/app.js', 'static/js/**/*.js'],
  css: ['static/css/main.bundle.css'],
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
      /^c-/,
      /^task-list-/,
      /^category-/,
      /^status-/,
      /^priority-/
    ],
    deep: [/^modal/, /^toast/],
    greedy: [/is-selected/]
  },
  output: 'static/css/main.bundle.min.css'
}
