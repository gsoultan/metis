import { Anchor, Button, type AnchorProps, type ButtonProps } from '@mantine/core';
import { createLink } from '@tanstack/react-router';
import type { ComponentPropsWithRef } from 'react';

/**
 * Mantine's button and anchor as router links.
 *
 * Built with createLink rather than by giving Mantine the router's Link as
 * `component`: that types `to` as any string, so a route that does not exist,
 * or one missing the search it requires, compiles. These check `to` and
 * `search` against the route tree, and pair with linkOptions(...).
 */

type ButtonAnchorProps = ButtonProps & Omit<ComponentPropsWithRef<'a'>, keyof ButtonProps>;

/** A button drawn as the link it is, so it can be opened in a new tab and is announced as a link. */
function ButtonAnchor(props: ButtonAnchorProps) {
  return <Button component="a" {...props} />;
}

type TextAnchorProps = AnchorProps & Omit<ComponentPropsWithRef<'a'>, keyof AnchorProps>;

function TextAnchor(props: TextAnchorProps) {
  return <Anchor {...props} />;
}

export const ButtonLink = createLink(ButtonAnchor);
export const AnchorLink = createLink(TextAnchor);
