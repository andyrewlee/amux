/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        medusa: {
          bg: '#0d1117',
          surface: '#161b22',
          border: '#30363d',
          text: '#e6edf3',
          muted: '#8b949e',
          accent: '#58a6ff',
          green: '#3fb950',
          red: '#f85149',
          yellow: '#d29922',
          purple: '#bc8cff',
        },
      },
    },
  },
  plugins: [],
}
