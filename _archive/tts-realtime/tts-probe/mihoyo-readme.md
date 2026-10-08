---
license: mit
language:
- zh
base_model:
- lj1995/GPT-SoVITS
pipeline_tag: text-to-speech
---
该项目中主要包含使用《原神》与《崩坏 星穹铁道》中部分角色的对话语音训练的用于GPT-SoVITS模型推理的模型以及部分参考音频。其中`GPT_models`与`VITS_models`为推理过程中使用的模型权重文件，文件夹以角色名命名。`ref_audios`中包含对应角色的推理参考音频。