import { ref } from "vue";

const lightboxSrc = ref<string | null>(null);

export function useImageLightbox() {
  function showImage(src: string) {
    lightboxSrc.value = src;
  }
  function closeImage() {
    lightboxSrc.value = null;
  }
  return { lightboxSrc, showImage, closeImage };
}
