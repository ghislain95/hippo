import {
  Button,
  Form,
  Host,
  Picker,
  Section,
  Slider,
  Text as SwiftText,
  Toggle,
} from '@expo/ui/swift-ui';
import { buttonStyle, pickerStyle, tag } from '@expo/ui/swift-ui/modifiers';
import { GlassContainer, GlassView, isLiquidGlassAvailable } from 'expo-glass-effect';
import type { GlassStyle } from 'expo-glass-effect';
import { useState } from 'react';
import { Platform, ScrollView, StyleSheet, Text, View } from 'react-native';

// Native look test screen (design D5): React Native views under liquid glass, then native
// SwiftUI controls, for Ghislain to judge on the iPhone before the E5 mock-ups.
const STYLES: GlassStyle[] = ['regular', 'clear'];
const TINTS = [undefined, '#ff6b3d55', '#3d8bff55'];

export default function GlassScreen() {
  const [style, setStyle] = useState<GlassStyle>('regular');
  const [tint, setTint] = useState(0);
  const [interactive, setInteractive] = useState(true);
  const [radius, setRadius] = useState(24);

  return (
    <ScrollView contentInsetAdjustmentBehavior="automatic" style={styles.screen}>
      <View style={styles.backdrop}>
        {BLOBS.map((blob) => (
          <View key={blob.color} style={[styles.blob, blob]} />
        ))}
        <GlassView
          style={[styles.card, { borderRadius: radius }]}
          glassEffectStyle={style}
          tintColor={TINTS[tint]}
          isInteractive={interactive}
        >
          <Text style={styles.title}>Une fiche sous verre</Text>
          <Text style={styles.body}>
            Recette · cuisine · 12 min de lecture{'\n'}Glisse le doigt dessus pour l'effet interactif.
          </Text>
        </GlassView>
        <GlassContainer spacing={12} style={styles.row}>
          {['Lu', 'Épinglé', 'Privé'].map((label) => (
            <GlassView key={label} style={styles.pill} glassEffectStyle={style} isInteractive>
              <Text style={styles.pillText}>{label}</Text>
            </GlassView>
          ))}
        </GlassContainer>
      </View>

      <Text style={styles.status}>
        iOS {String(Platform.Version)} · verre liquide {isLiquidGlassAvailable() ? 'disponible' : 'indisponible'}
      </Text>

      <Host style={styles.host}>
        <Form>
          <Section title="Réglages du verre (SwiftUI)">
            <Picker
              label="Style"
              selection={style}
              onSelectionChange={(value: GlassStyle) => setStyle(value)}
              modifiers={[pickerStyle('segmented')]}
            >
              {STYLES.map((value) => (
                <SwiftText key={value} modifiers={[tag(value)]}>
                  {value}
                </SwiftText>
              ))}
            </Picker>
            <Picker
              label="Teinte"
              selection={tint}
              onSelectionChange={(value: number) => setTint(value)}
              modifiers={[pickerStyle('menu')]}
            >
              {['Aucune', 'Orange', 'Bleu'].map((label, index) => (
                <SwiftText key={label} modifiers={[tag(index)]}>
                  {label}
                </SwiftText>
              ))}
            </Picker>
            <Toggle label="Interactif" isOn={interactive} onIsOnChange={setInteractive} />
            <Slider value={radius} min={0} max={48} step={1} onValueChange={setRadius} />
          </Section>
          <Section title="Boutons natifs">
            <Button label="Bouton verre" systemImage="sparkles" modifiers={[buttonStyle('glass')]} />
            <Button
              label="Bouton verre accentué"
              systemImage="tray.and.arrow.down"
              modifiers={[buttonStyle('glassProminent')]}
            />
          </Section>
        </Form>
      </Host>
    </ScrollView>
  );
}

const BLOBS = [
  { color: '#ff6b3d', backgroundColor: '#ff6b3d', top: 10, left: -20 },
  { color: '#3d8bff', backgroundColor: '#3d8bff', top: 90, right: -30 },
  { color: '#2fc77a', backgroundColor: '#2fc77a', top: 200, left: 80 },
];

const styles = StyleSheet.create({
  screen: { flex: 1 },
  backdrop: { height: 340, margin: 16, overflow: 'hidden', borderRadius: 28 },
  blob: { position: 'absolute', width: 180, height: 180, borderRadius: 90 },
  card: { marginTop: 40, marginHorizontal: 24, padding: 20 },
  title: { fontSize: 20, fontWeight: '600' },
  body: { marginTop: 6, fontSize: 14, opacity: 0.8 },
  row: { flexDirection: 'row', gap: 12, marginTop: 24, marginHorizontal: 24 },
  pill: { paddingHorizontal: 16, paddingVertical: 10, borderRadius: 20 },
  pillText: { fontSize: 15, fontWeight: '500' },
  status: { marginHorizontal: 16, fontSize: 13, opacity: 0.6 },
  host: { height: 420, marginTop: 8 },
});
