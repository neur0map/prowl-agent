type JSDOMEnvironment = {
  window: Window
}

const jsdom = (globalThis as typeof globalThis & { jsdom?: JSDOMEnvironment }).jsdom

if (jsdom) {
  Object.defineProperties(globalThis, {
    localStorage: {
      configurable: true,
      get: () => jsdom.window.localStorage,
    },
    sessionStorage: {
      configurable: true,
      get: () => jsdom.window.sessionStorage,
    },
  })
}
