import { BodyMarkdown } from '../BodyMarkdown';
import type { RendererProps } from './types';

export function GenericSheet({ artifact, previous, mode }: RendererProps) {
  return <BodyMarkdown body={artifact.body} previous={previous?.body} mode={mode} version={artifact.version} />;
}
