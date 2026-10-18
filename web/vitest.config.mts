import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.{ts,tsx}', 'scripts/**/*.test.mjs'],
    restoreMocks: true,
  },
})
