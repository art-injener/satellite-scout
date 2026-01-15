module.exports = {
    root: true,
    env: {
        browser: true,
        es2021: true
    },
    parserOptions: {
        ecmaVersion: 2021,
        sourceType: 'script'
    },
    extends: [
        'eslint:recommended'
    ],
    rules: {
        'no-var': 'error',
        'prefer-const': 'error',
        'eqeqeq': ['error', 'always'],
        'curly': 'error',
        'no-unused-vars': ['error', { args: 'after-used', ignoreRestSiblings: false }],
        'no-use-before-define': ['error', { functions: false, classes: true, variables: true }],
        'no-redeclare': 'error',
        'no-shadow': 'error',
        'no-undef': 'error',
        'no-implicit-coercion': 'error',
        'no-multi-spaces': 'error',
        'no-trailing-spaces': 'error',
        'no-extra-semi': 'error',
        'no-throw-literal': 'error',
        'no-unused-expressions': 'error',
        'no-console': 'warn',
        'no-debugger': 'error',
        'consistent-return': 'error',
        'radix': 'error',
        'yoda': 'error',
        'semi': ['error', 'always'],
        'quotes': ['error', 'single', { avoidEscape: true, allowTemplateLiterals: true }],
        'indent': ['error', 4, { SwitchCase: 1 }],
        'object-curly-spacing': ['error', 'always'],
        'array-bracket-spacing': ['error', 'never'],
        'space-infix-ops': 'error',
        'keyword-spacing': ['error', { before: true, after: true }],
        'space-before-blocks': 'error',
        'space-before-function-paren': ['error', { anonymous: 'never', named: 'never', asyncArrow: 'always' }],
        'eol-last': 'error'
    }
};
