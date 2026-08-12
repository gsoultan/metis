import { useForm } from '@tanstack/react-form';
import type { AnyFieldApi } from '@tanstack/react-form';
import type { ZodType } from 'zod';

/**
 * Thin adapter between TanStack Form and Mantine inputs.
 *
 * Two things it exists to enforce:
 *
 *  1. **Validation lives in a Zod schema**, not scattered across `validate`
 *     callbacks. The same schema can then describe the API payload, so the
 *     form and the request cannot disagree about what is required.
 *
 *  2. **Errors are announced, not just coloured.** Mantine renders `error` text
 *     but a screen-reader user needs the association: `aria-invalid` on the
 *     input and `role="alert"` on the message. `fieldProps` does that once
 *     rather than at 40 call sites.
 */

/** Validator that runs a Zod schema against a single field value. */
export function zodField<T>(schema: ZodType<T>) {
  return ({ value }: { value: T }) => {
    const result = schema.safeParse(value);
    return result.success ? undefined : result.error.issues[0]?.message;
  };
}

/**
 * Maps a TanStack field onto the props a Mantine input expects, including the
 * accessibility attributes that make a validation error perceivable rather
 * than merely visible.
 */
export function fieldProps(field: AnyFieldApi) {
  const errors = field.state.meta.errors.filter(Boolean);
  const showError = field.state.meta.isTouched && errors.length > 0;
  const message = showError ? String(errors[0]) : undefined;

  return {
    name: field.name,
    value: field.state.value as string,
    onChange: (event: React.ChangeEvent<HTMLInputElement>) =>
      field.handleChange(event.currentTarget.value),
    onBlur: field.handleBlur,
    error: message,
    'aria-invalid': showError || undefined,
    'aria-errormessage': showError ? `${field.name}-error` : undefined,
    errorProps: { id: `${field.name}-error`, role: 'alert' as const },
  };
}

export { useForm };
