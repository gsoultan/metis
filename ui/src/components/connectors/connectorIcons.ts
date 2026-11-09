import { BellRing, Globe, Mail, MessageSquare, Send, Users, Zap, type LucideIcon } from 'lucide-react';

/** The icons a connector may name, by the name the catalogue stores. */
export const CONNECTOR_ICONS: Record<string, LucideIcon> = {
  BellRing,
  Globe,
  MessageSquare,
  Mail,
  Send,
  Users,
  Zap,
};

/** The icon for a connector, with a generic one for a name nobody drew. */
export function connectorIcon(name?: string): LucideIcon {
  return (name ? CONNECTOR_ICONS[name] : undefined) ?? Zap;
}
