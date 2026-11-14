// https://docs.expo.dev/guides/using-eslint/
const { defineConfig } = require('eslint/config');
const expoConfig = require('eslint-config-expo/flat');

module.exports = defineConfig([
  expoConfig,
  {
    ignores: [
      'dist/*',
      'coverage/*',
      'build/*',
      'ios/*',
      'targets/.build/*',
      'src/api/schema.gen.ts',
    ],
  },
  {
    // The config plugins and their tests are CommonJS run by Node (plugins/, design D1): jest's
    // globals are declared here because the TypeScript block of eslint-config-expo, which
    // relaxes no-undef, does not apply to .js files.
    files: ['plugins/**/*.test.js'],
    languageOptions: {
      globals: { describe: 'readonly', test: 'readonly', expect: 'readonly' },
    },
  },
]);
