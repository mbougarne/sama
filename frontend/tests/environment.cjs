// React Router uses these browser APIs, which jsdom does not supply.
const { TextDecoder, TextEncoder } = require('node:util');
Object.assign(globalThis, { TextDecoder, TextEncoder });
