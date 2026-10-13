import rehypeAttrs from 'rehype-attr';
import rehypeIgnore from 'rehype-ignore';
import rehypeRaw from 'rehype-raw';
import type { MarkdownPreviewProps } from '@uiw/react-markdown-preview';
import { safeUrlTransform } from '../../utils/safeUrlTransform';

/**
 * Plugins @uiw/react-markdown-preview always injects that let a body author
 * control the page: rehype-raw parses raw HTML into live elements before
 * skipHtml can drop it, rehype-attr copies `<!--rehype:k=v-->` comments onto
 * elements as attributes, and rehype-ignore hides text from the human reader
 * that an agent still receives. Matched by identity, so each stays pinned to
 * the copy the library resolves.
 */
const unsafePlugins = new Set<unknown>([rehypeRaw, rehypeAttrs, rehypeIgnore]);

const dropUnsafePlugins: NonNullable<MarkdownPreviewProps['pluginsFilter']> = (type, plugins) =>
  type === 'rehype'
    ? plugins.filter((plugin) => !unsafePlugins.has(Array.isArray(plugin) ? plugin[0] : plugin))
    : plugins;

export const safeMarkdownOptions = {
  skipHtml: true,
  urlTransform: safeUrlTransform,
  pluginsFilter: dropUnsafePlugins,
} satisfies MarkdownPreviewProps;
