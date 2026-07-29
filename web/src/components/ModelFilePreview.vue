<template>
  <div ref="container" class="model-file-preview">
    <canvas ref="canvas" class="model-file-preview__canvas" :aria-label="`3D 模型 ${filename}`" />
    <q-spinner v-if="loading" class="model-file-preview__loading" color="primary" size="32px" />
    <q-banner
      v-if="error"
      dense
      class="model-file-preview__error app-feedback app-feedback--danger"
    >
      {{ error }}
    </q-banner>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js';
import { OBJLoader } from 'three/addons/loaders/OBJLoader.js';
import { STLLoader } from 'three/addons/loaders/STLLoader.js';
import { ThreeMFLoader } from 'three/addons/loaders/3MFLoader.js';

import { modelFileFormat } from '@/services/modelFiles';

const props = defineProps<{ src: string; filename: string }>();
const container = ref<HTMLElement | null>(null);
const canvas = ref<HTMLCanvasElement | null>(null);
const loading = ref(false);
const error = ref('');
let renderer: THREE.WebGLRenderer | null = null;
let scene: THREE.Scene | null = null;
let camera: THREE.PerspectiveCamera | null = null;
let controls: OrbitControls | null = null;
let resizeObserver: ResizeObserver | null = null;
let currentObject: THREE.Object3D | null = null;
let mixer: THREE.AnimationMixer | null = null;
let animationFrame = 0;
let loadGeneration = 0;
const clock = new THREE.Clock();

onMounted(() => {
  try {
    setupScene();
    void loadModel();
  } catch (err) {
    error.value = errorMessage(err, '当前浏览器无法初始化 3D 预览');
  }
});

watch(
  () => [props.src, props.filename],
  () => void loadModel(),
);

onBeforeUnmount(() => {
  loadGeneration++;
  cancelAnimationFrame(animationFrame);
  resizeObserver?.disconnect();
  controls?.dispose();
  clearModel();
  renderer?.dispose();
  renderer?.forceContextLoss();
});

function setupScene() {
  const element = canvas.value;
  const host = container.value;
  if (!element || !host) return;

  scene = new THREE.Scene();
  camera = new THREE.PerspectiveCamera(45, 1, 0.01, 1000);
  renderer = new THREE.WebGLRenderer({ canvas: element, antialias: true, alpha: true });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
  renderer.outputColorSpace = THREE.SRGBColorSpace;
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.toneMappingExposure = 1.1;

  scene.add(new THREE.HemisphereLight(0xffffff, 0x667085, 2.4));
  const keyLight = new THREE.DirectionalLight(0xffffff, 2.8);
  keyLight.position.set(4, 6, 5);
  scene.add(keyLight);

  controls = new OrbitControls(camera, element);
  controls.enableDamping = true;
  controls.dampingFactor = 0.08;

  resizeObserver = new ResizeObserver(resize);
  resizeObserver.observe(host);
  resize();
  animate();
}

async function loadModel() {
  const activeScene = scene;
  const format = modelFileFormat(props.filename);
  const source = props.src;
  const generation = ++loadGeneration;
  clearModel();
  error.value = '';
  if (!activeScene || !source) return;
  if (!format) {
    error.value = '不支持此 3D 模型格式';
    return;
  }

  loading.value = true;
  try {
    const loaded = await readModel(source, format);
    if (generation !== loadGeneration) {
      disposeObject(loaded.object);
      return;
    }
    currentObject = loaded.object;
    activeScene.add(loaded.object);
    fitCamera(loaded.object);
    if (loaded.animations.length > 0) {
      mixer = new THREE.AnimationMixer(loaded.object);
      for (const clip of loaded.animations) mixer.clipAction(clip).play();
    }
  } catch (err) {
    if (generation === loadGeneration) {
      clearModel();
      error.value = errorMessage(err, '加载 3D 模型失败');
    }
  } finally {
    if (generation === loadGeneration) loading.value = false;
  }
}

async function readModel(source: string, format: NonNullable<ReturnType<typeof modelFileFormat>>) {
  const manager = new THREE.LoadingManager();
  manager.setURLModifier((url) => {
    if (url === source || url.startsWith('blob:') || url.startsWith('data:')) return url;
    throw new Error('模型引用了不支持的外部资源');
  });
  switch (format) {
    case 'glb':
    case 'gltf': {
      const result = await new GLTFLoader(manager).loadAsync(source);
      return { object: result.scene, animations: result.animations };
    }
    case 'obj':
      return { object: await new OBJLoader(manager).loadAsync(source), animations: [] };
    case 'stl': {
      const geometry = await new STLLoader(manager).loadAsync(source);
      if (!geometry.getAttribute('normal')) geometry.computeVertexNormals();
      const material = new THREE.MeshStandardMaterial({
        color: 0x9aa5b1,
        roughness: 0.65,
        metalness: 0.05,
        vertexColors: geometry.hasAttribute('color'),
      });
      return { object: new THREE.Mesh(geometry, material), animations: [] };
    }
    case '3mf':
      return { object: await new ThreeMFLoader(manager).loadAsync(source), animations: [] };
  }
}

function fitCamera(object: THREE.Object3D) {
  if (!camera || !controls) return;
  const box = new THREE.Box3().setFromObject(object);
  if (box.isEmpty()) throw new Error('模型中没有可显示的几何体');
  const center = box.getCenter(new THREE.Vector3());
  const size = box.getSize(new THREE.Vector3());
  object.position.sub(center);

  const radius = Math.max(size.length() / 2, 0.01);
  const distance = (radius / Math.sin(THREE.MathUtils.degToRad(camera.fov / 2))) * 1.15;
  camera.near = Math.max(radius / 100, 0.001);
  camera.far = radius * 100;
  camera.position.set(distance * 0.8, distance * 0.6, distance);
  camera.updateProjectionMatrix();
  controls.target.set(0, 0, 0);
  controls.minDistance = radius * 0.15;
  controls.maxDistance = radius * 20;
  controls.update();
}

function resize() {
  const host = container.value;
  if (!host || !renderer || !camera) return;
  const width = Math.max(1, host.clientWidth);
  const height = Math.max(1, host.clientHeight);
  renderer.setSize(width, height, false);
  camera.aspect = width / height;
  camera.updateProjectionMatrix();
}

function animate() {
  animationFrame = requestAnimationFrame(animate);
  const delta = clock.getDelta();
  mixer?.update(delta);
  controls?.update();
  if (renderer && scene && camera) renderer.render(scene, camera);
}

function clearModel() {
  mixer?.stopAllAction();
  mixer = null;
  if (!currentObject) return;
  scene?.remove(currentObject);
  disposeObject(currentObject);
  currentObject = null;
}

function disposeObject(object: THREE.Object3D) {
  object.traverse((child) => {
    const mesh = child as THREE.Mesh;
    mesh.geometry?.dispose();
    const materials = Array.isArray(mesh.material)
      ? mesh.material
      : mesh.material
        ? [mesh.material]
        : [];
    for (const material of materials) {
      for (const value of Object.values(material)) {
        if (value instanceof THREE.Texture) value.dispose();
      }
      material.dispose();
    }
  });
}

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error && err.message ? err.message : fallback;
}
</script>

<style scoped>
.model-file-preview {
  position: relative;
  width: 100%;
  height: 100%;
  min-height: 260px;
  overflow: hidden;
  touch-action: none;
}

.model-file-preview__canvas {
  display: block;
  width: 100%;
  height: 100%;
}

.model-file-preview__loading {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
}

.model-file-preview__error {
  position: absolute;
  right: 12px;
  bottom: 12px;
  left: 12px;
}
</style>
