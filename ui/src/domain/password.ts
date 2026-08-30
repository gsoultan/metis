/**
 * The minimum password length, matching MinPasswordLength in
 * server/domains/services/impl/user.go.
 *
 * Duplicated rather than fetched: it is a constant, and a form that only learns
 * the rule from a rejection makes the user guess. `passwordRuleMatchesServer`
 * in the test pins the two together.
 */
export const MIN_PASSWORD_LENGTH = 8;
