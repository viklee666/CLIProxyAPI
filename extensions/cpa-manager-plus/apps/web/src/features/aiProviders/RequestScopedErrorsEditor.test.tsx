import { useState } from 'react';
import { act } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';
import { describe, expect, it, vi } from 'vitest';
import { Input } from '@/components/ui/Input';
import { Button } from '@/components/ui/Button';
import { Select } from '@/components/ui/Select';
import { RequestScopedErrorsEditor } from './RequestScopedErrorsEditor';

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

describe('RequestScopedErrorsEditor', () => {
  it('adds, edits and removes rules without entering a JSON array', () => {
    let value = '[]';
    function Editor() {
      const [text, setText] = useState(value);
      return (
        <RequestScopedErrorsEditor
          value={text}
          onChange={(next) => {
            value = next;
            setText(next);
          }}
        />
      );
    }
    let renderer!: ReactTestRenderer;
    act(() => {
      renderer = create(<Editor />);
    });
    const button = (key: string) =>
      renderer.root.findAllByType(Button).find((node) => node.props.children === key)!;
    expect(renderer.root.findAllByType('textarea')).toHaveLength(0);
    act(() => button('ai_providers.error_rule_add').props.onClick());
    expect(JSON.parse(value)).toEqual([{ status: 429, action: 'continue-and-cooldown' }]);

    // Intermediate status digits must leave the form editable while validation is shown.
    act(() => renderer.root.findByType(Input).props.onChange({ target: { value: '4' } }));
    expect(renderer.root.findAllByType('fieldset')).toHaveLength(1);
    expect(renderer.root.findAllByProps({ role: 'alert' })).toHaveLength(1);
    act(() => renderer.root.findByType(Input).props.onChange({ target: { value: '400' } }));
    act(() =>
      renderer.root
        .findAllByType('textarea')[0]
        .props.onChange({ target: { value: 'quota exceeded\ncapacity' } })
    );
    expect(renderer.root.findAllByProps({ role: 'alert' })).toHaveLength(0);
    act(() =>
      renderer.root
        .findAllByType('textarea')[1]
        .props.onChange({ target: { value: 'limit.*reached' } })
    );
    act(() => renderer.root.findByType(Select).props.onChange('stop-and-cooldown'));
    expect(JSON.parse(value)).toEqual([
      {
        status: 400,
        action: 'stop-and-cooldown',
        match: ['quota exceeded', 'capacity'],
        matchRegexr: ['limit.*reached'],
      },
    ]);
    act(() => button('ai_providers.error_rule_remove').props.onClick());
    expect(value).toBe('[]');
    act(() => renderer.unmount());
  });
});
