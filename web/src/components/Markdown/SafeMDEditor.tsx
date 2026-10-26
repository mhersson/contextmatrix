import MDEditor, { type MDEditorProps } from '@uiw/react-md-editor/common';
import { safeMarkdownOptions } from './safeMarkdownOptions';

type SafeMDEditorProps = Omit<MDEditorProps, 'previewOptions' | 'components'> & {
  components?: Omit<NonNullable<MDEditorProps['components']>, 'preview'> & { preview?: never };
};

/** MDEditor whose Live and Preview panes render through the same safe options
 *  as SafeMarkdownPreview. */
export default function SafeMDEditor(props: SafeMDEditorProps) {
  return <MDEditor {...props} previewOptions={safeMarkdownOptions} />;
}
