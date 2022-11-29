/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package validate

import (
	"context"
	"fmt"
	"runtime"
	"sort"

	"github.com/kubernetes-sigs/cri-tools/pkg/framework"
	internalapi "k8s.io/cri-api/pkg/apis"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = framework.KubeDescribe("Image Manager", func() {
	f := framework.NewDefaultCRIFramework()

	ctx := context.Background()
	var c internalapi.ImageManagerService

	BeforeEach(func() {
		c = f.CRIClient.CRIImageClient
	})

	It("public image with tag should be pulled and removed [Conformance]", func() {
		testPullPublicImage(ctx, c, testImageWithTag, testImagePodSandbox, func(s *runtimeapi.Image) {
			Expect(s.RepoTags).To(Equal([]string{testImageWithTag}))
		})
	})

	It("public image without tag should be pulled and removed [Conformance]", func() {
		testPullPublicImage(ctx, c, testImageWithoutTag, testImagePodSandbox, func(s *runtimeapi.Image) {
			Expect(s.RepoTags).To(Equal([]string{testImageWithoutTag + ":latest"}))
		})
	})

	It("public image with digest should be pulled and removed [Conformance]", func() {
		testPullPublicImage(ctx, c, testImageWithDigest, testImagePodSandbox, func(s *runtimeapi.Image) {
			Expect(s.RepoTags).To(BeEmpty())
			Expect(s.RepoDigests).To(Equal([]string{testImageWithDigest}))
		})
	})

	It("image status should support all kinds of references [Conformance]", func() {
		imageName := testImageWithAllReferences
		// Make sure image does not exist before testing.
		removeImage(ctx, c, imageName)

		framework.PullPublicImage(ctx, c, imageName, testImagePodSandbox)

		status := framework.ImageStatus(ctx, c, imageName)
		Expect(status).NotTo(BeNil(), "should get image status")
		idStatus := framework.ImageStatus(ctx, c, status.GetId())
		Expect(idStatus).To(Equal(status), "image status with %q", status.GetId())
		for _, tag := range status.GetRepoTags() {
			tagStatus := framework.ImageStatus(ctx, c, tag)
			Expect(tagStatus).To(Equal(status), "image status with %q", tag)
		}
		for _, digest := range status.GetRepoDigests() {
			digestStatus := framework.ImageStatus(ctx, c, digest)
			Expect(digestStatus).To(Equal(status), "image status with %q", digest)
		}

		testRemoveImage(ctx, c, imageName)
	})

	if runtime.GOOS != "windows" || framework.TestContext.IsLcow {
		It("image status get image fields should not have Uid|Username empty [Conformance]", func() {
			for _, item := range []struct {
				description string
				image       string
				uid         int64
				username    string
			}{
				{
					description: "UID only",
					image:       testImageUserUID,
					uid:         imageUserUID,
					username:    "",
				},
				{
					description: "Username only",
					image:       testImageUserUsername,
					uid:         int64(0),
					username:    imageUserUsername,
				},
				{
					description: "UID:group",
					image:       testImageUserUIDGroup,
					uid:         imageUserUIDGroup,
					username:    "",
				},
				{
					description: "Username:group",
					image:       testImageUserUsernameGroup,
					uid:         int64(0),
					username:    imageUserUsernameGroup,
				},
			} {
				framework.PullPublicImage(ctx, c, item.image, testImagePodSandbox)
				defer removeImage(ctx, c, item.image)

				status := framework.ImageStatus(ctx, c, item.image)
				Expect(status.GetUid().GetValue()).To(Equal(item.uid), fmt.Sprintf("%s, Image Uid should be %d", item.description, item.uid))
				Expect(status.GetUsername()).To(Equal(item.username), fmt.Sprintf("%s, Image Username should be %s", item.description, item.username))
			}
		})
	}

	It("listImage should get exactly 3 image in the result list [Conformance]", func() {
		// Make sure test image does not exist.
		removeImageList(ctx, c, testDifferentTagDifferentImageList)
		ids := pullImageList(ctx, c, testDifferentTagDifferentImageList, testImagePodSandbox)
		ids = removeDuplicates(ids)
		Expect(len(ids)).To(Equal(3), "3 image ids should be returned")

		defer removeImageList(ctx, c, testDifferentTagDifferentImageList)

		images := framework.ListImage(ctx, c, &runtimeapi.ImageFilter{})

		for i, id := range ids {
			for _, img := range images {
				if img.Id == id {
					Expect(len(img.RepoTags)).To(Equal(1), "Should only have 1 repo tag")
					Expect(img.RepoTags[0]).To(Equal(testDifferentTagDifferentImageList[i]), "Repo tag should be correct")
					break
				}
			}
		}
	})

	It("listImage should get exactly 3 repoTags in the result image [Conformance]", func() {
		// Make sure test image does not exist.
		removeImageList(ctx, c, testDifferentTagSameImageList)
		ids := pullImageList(ctx, c, testDifferentTagSameImageList, testImagePodSandbox)
		ids = removeDuplicates(ids)
		Expect(len(ids)).To(Equal(1), "Only 1 image id should be returned")

		defer removeImageList(ctx, c, testDifferentTagSameImageList)

		images := framework.ListImage(ctx, c, &runtimeapi.ImageFilter{})

		sort.Strings(testDifferentTagSameImageList)
		for _, img := range images {
			if img.Id == ids[0] {
				sort.Strings(img.RepoTags)
				Expect(img.RepoTags).To(Equal(testDifferentTagSameImageList), "Should have 3 repoTags in single image")
				break
			}
		}
	})
})

// testRemoveImage removes the image name imageName and check if it successes.
func testRemoveImage(ctx context.Context, c internalapi.ImageManagerService, imageName string) {
	By("Remove image : " + imageName)
	removeImage(ctx, c, imageName)

	By("Check image list empty")
	status := framework.ImageStatus(ctx, c, imageName)
	Expect(status).To(BeNil(), "Should have none image in list")
}

// testPullPublicImage pulls the image named imageName, make sure it success and remove the image.
func testPullPublicImage(ctx context.Context, c internalapi.ImageManagerService, imageName string, podConfig *runtimeapi.PodSandboxConfig, statusCheck func(*runtimeapi.Image)) {
	// Make sure image does not exist before testing.
	removeImage(ctx, c, imageName)

	framework.PullPublicImage(ctx, c, imageName, podConfig)

	By("Check image list to make sure pulling image success : " + imageName)
	status := framework.ImageStatus(ctx, c, imageName)
	Expect(status).NotTo(BeNil(), "Should have one image in list")
	Expect(status.Id).NotTo(BeNil(), "Image Id should not be nil")
	Expect(status.Size_).NotTo(BeNil(), "Image Size should not be nil")
	if statusCheck != nil {
		statusCheck(status)
	}

	testRemoveImage(ctx, c, imageName)
}

// pullImageList pulls the images listed in the imageList.
func pullImageList(ctx context.Context, c internalapi.ImageManagerService, imageList []string, podConfig *runtimeapi.PodSandboxConfig) []string {
	ids := []string{}
	for _, imageName := range imageList {
		ids = append(ids, framework.PullPublicImage(ctx, c, imageName, podConfig))
	}
	return ids
}

// removeImageList removes the images listed in the imageList.
func removeImageList(ctx context.Context, c internalapi.ImageManagerService, imageList []string) {
	for _, imageName := range imageList {
		removeImage(ctx, c, imageName)
	}
}

// removeImage removes the image named imagesName.
func removeImage(ctx context.Context, c internalapi.ImageManagerService, imageName string) {
	By("Remove image : " + imageName)
	image, err := c.ImageStatus(ctx, &runtimeapi.ImageSpec{Image: imageName}, false)
	framework.ExpectNoError(err, "failed to get image status: %v", err)

	if image.Image != nil {
		By("Remove image by ID : " + image.Image.Id)
		err = c.RemoveImage(ctx, &runtimeapi.ImageSpec{Image: image.Image.Id})
		framework.ExpectNoError(err, "failed to remove image: %v", err)
	}
}

// removeDuplicates remove duplicates strings from a list
func removeDuplicates(ss []string) []string {
	encountered := map[string]bool{}
	result := []string{}
	for _, s := range ss {
		if encountered[s] == true {
			continue
		}
		encountered[s] = true
		result = append(result, s)
	}
	return result
}
