import { Button, Tooltip, type ButtonProps } from '@mantine/core';
import type { ReactNode } from 'react';

interface ComingSoonButtonProps extends ButtonProps {
  children: ReactNode;
  /** What this will do once it exists. Shown on hover and to screen readers. */
  label?: string;
}

/**
 * A control for a feature that is not built yet.
 *
 * The alternative this replaces is a `<Button>` with no `onClick`, which looks
 * identical to a working one and does nothing when pressed. That is the worst
 * of the three options available:
 *
 *   - not rendering it     → the user never expects the feature
 *   - rendering it here    → the user knows it is coming and why it is inert
 *   - rendering it dead    → the user concludes the product is broken
 *
 * `aria-disabled` rather than `disabled` keeps the control focusable, so a
 * keyboard or screen-reader user can reach it and hear the explanation instead
 * of skipping past an element they cannot perceive.
 */
export function ComingSoonButton({ children, label, ...props }: ComingSoonButtonProps) {
  const explanation = label ?? 'This is not available yet';

  return (
    <Tooltip label={explanation} withArrow>
      <Button
        {...props}
        aria-disabled
        data-disabled
        onClick={(event) => event.preventDefault()}
      >
        {children}
      </Button>
    </Tooltip>
  );
}
