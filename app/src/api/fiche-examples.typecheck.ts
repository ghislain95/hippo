// Type-level check, run by `tsc --noEmit` (make typecheck-app): every fiche example of
// docs/prompt/schemas/examples must be a valid value of the generated Fiche type, and the union
// must narrow by template. Nothing imports this file at run time.
import type { components } from './schema.gen';

import generic from '../../../docs/prompt/schemas/examples/generic.json';
import note from '../../../docs/prompt/schemas/examples/note.json';
import place from '../../../docs/prompt/schemas/examples/place.json';
import recipe from '../../../docs/prompt/schemas/examples/recipe.json';
import resource from '../../../docs/prompt/schemas/examples/resource.json';
import techArticle from '../../../docs/prompt/schemas/examples/tech_article.json';
import tip from '../../../docs/prompt/schemas/examples/tip.json';
import tutorial from '../../../docs/prompt/schemas/examples/tutorial.json';

type Fiche = components['schemas']['Fiche'];

// JSON imports widen string literals, so each template is restated as a literal; every other
// field is checked as it is written in the example. The examples' own template values are
// checked by the Go test TestFicheExamplesDecode and by schemabundle.
export const ficheExamples: Fiche[] = [
  { ...generic.fiche, template: 'generic' },
  { ...note.fiche, template: 'note' },
  { ...place.fiche, template: 'place' },
  { ...recipe.fiche, template: 'recipe' },
  { ...resource.fiche, template: 'resource' },
  { ...techArticle.fiche, template: 'tech_article' },
  { ...tip.fiche, template: 'tip' },
  { ...tutorial.fiche, template: 'tutorial' },
];

/** Narrowing by template gives access to the fields of each fiche type. */
export function headline(fiche: Fiche): string {
  switch (fiche.template) {
    case 'tech_article':
      return fiche.tldr;
    case 'tutorial':
      return fiche.goal;
    case 'tip':
      return fiche.tip;
    case 'recipe':
      return fiche.ingredients.map((ingredient) => ingredient.name).join(', ');
    case 'place':
      return fiche.maps_query;
    case 'resource':
      return fiche.what_it_is;
    case 'note':
      return fiche.idea;
    case 'generic':
      return fiche.summary;
  }
}
