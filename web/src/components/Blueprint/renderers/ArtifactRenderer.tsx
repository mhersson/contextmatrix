import { AcceptanceSheet } from './AcceptanceSheet';
import { BoardSheet } from './BoardSheet';
import { CardsSheet } from './CardsSheet';
import { DesignSheet } from './DesignSheet';
import { GenericSheet } from './GenericSheet';
import { GroundingSheet } from './GroundingSheet';
import { NeedSheet } from './NeedSheet';
import { ReadinessSheet } from './ReadinessSheet';
import { SignoffSheet } from './SignoffSheet';
import { SharedDecisionsSheet } from './SharedDecisionsSheet';
import { SlicesSheet } from './SlicesSheet';
import type { RendererProps } from './types';

export function ArtifactRenderer(props: RendererProps) {
  switch (props.artifact.stage) {
    case 'intake':
      return <NeedSheet {...props} />;
    case 'slicing':
      return <SlicesSheet {...props} />;
    case 'shared_decisions':
      return <SharedDecisionsSheet {...props} />;
    case 'solution_design':
      return <DesignSheet {...props} />;
    case 'grounding':
      return <GroundingSheet {...props} />;
    case 'acceptance':
      return <AcceptanceSheet {...props} />;
    case 'decomposition':
      return <CardsSheet {...props} />;
    case 'readiness':
      return <ReadinessSheet {...props} />;
    case 'signoff':
      return <SignoffSheet {...props} />;
    case 'emission':
      return <BoardSheet {...props} />;
    default:
      return <GenericSheet {...props} />;
  }
}
