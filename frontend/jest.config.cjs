module.exports = {
  testEnvironment: 'jsdom',
  // Use Node's file crawler; no external Watchman service is required.
  watchman: false,
  roots: ['<rootDir>/tests'],
  testMatch: ['**/*.test.ts?(x)'],
  setupFiles: ['<rootDir>/tests/environment.cjs'],
  setupFilesAfterEnv: ['<rootDir>/tests/setup.ts'],
  extensionsToTreatAsEsm: ['.ts', '.tsx'],
  transform: {
    '^.+\\.(m?js|jsx|ts|tsx)$': [
      'babel-jest',
      {
        presets: [
          [
            '@babel/preset-env',
            { targets: { node: 'current' }, modules: false },
          ],
          ['@babel/preset-react', { runtime: 'automatic' }],
          '@babel/preset-typescript',
        ],
      },
    ],
  },
  clearMocks: true,
  restoreMocks: true,
};
