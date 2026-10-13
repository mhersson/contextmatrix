import MDEditor, { type MDEditorProps } from '@uiw/react-md-editor';
import { safeMarkdownOptions } from './safeMarkdownOptions';

/** MDEditor whose Live and Preview panes render through the same safe options
 *  as SafeMarkdownPreview. */
export default function SafeMDEditor(props: Omit<MDEditorProps, 'previewOptions'>) {
  return <MDEditor {...props} previewOptions={safeMarkdownOptions} />;
}
